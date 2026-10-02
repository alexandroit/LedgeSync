//go:build linux

package credentialvault

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretDestination = "org.freedesktop.secrets"
	secretPath        = dbus.ObjectPath("/org/freedesktop/secrets")
	secretService     = "org.freedesktop.Secret.Service"
	secretCollection  = "org.freedesktop.Secret.Collection"
	secretItem        = "org.freedesktop.Secret.Item"
	secretPrompt      = "org.freedesktop.Secret.Prompt"
	operationTimeout  = 30 * time.Second
)

type linuxStore struct{}
type linuxSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}
type secretConnection struct {
	conn   *dbus.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

func newNativeStore() Store { return linuxStore{} }

// unixBusAddress intentionally rejects TCP, autolaunch, and multiple-address
// fallbacks. The plain Secret Service session is restricted to local Unix IPC.
func unixBusAddress(address string) (string, error) {
	if !strings.HasPrefix(address, "unix:") || strings.Contains(address, ";") {
		return "", ErrUnavailable
	}
	var path string
	for _, option := range strings.Split(strings.TrimPrefix(address, "unix:"), ",") {
		key, value, ok := strings.Cut(option, "=")
		if !ok {
			return "", ErrUnavailable
		}
		decoded, err := url.PathUnescape(value)
		if err != nil || strings.ContainsRune(decoded, 0) {
			return "", ErrUnavailable
		}
		switch key {
		case "path":
			if path != "" || !strings.HasPrefix(decoded, "/") {
				return "", ErrUnavailable
			}
			path = decoded
		case "abstract":
			if path != "" || decoded == "" {
				return "", ErrUnavailable
			}
			path = "\x00" + decoded
		case "guid":
			// Bus authentication and Hello still run; GUID is not secret data.
		default:
			return "", ErrUnavailable
		}
	}
	if path == "" {
		return "", ErrUnavailable
	}
	return path, nil
}

func connectSecretService() (*secretConnection, error) {
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if address == "" {
		// Standard systemd user bus; no shell-based dbus-launch discovery.
		address = "unix:path=/run/user/" + strconv.Itoa(os.Getuid()) + "/bus"
	}
	path, err := unixBusAddress(address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	socket, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		cancel()
		return nil, ErrUnavailable
	}
	deadline, _ := ctx.Deadline()
	if err = socket.SetDeadline(deadline); err != nil {
		_ = socket.Close()
		cancel()
		return nil, ErrUnavailable
	}
	conn, err := dbus.NewConn(socket, dbus.WithContext(ctx))
	if err != nil {
		_ = socket.Close()
		cancel()
		return nil, ErrUnavailable
	}
	s := &secretConnection{conn: conn, ctx: ctx, cancel: cancel}
	if err = conn.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Getuid()))}); err != nil {
		s.close()
		return nil, ErrUnavailable
	}
	if err = conn.Hello(); err != nil {
		s.close()
		return nil, ErrUnavailable
	}
	return s, nil
}

func (s *secretConnection) close() { _ = s.conn.Close(); s.cancel() }
func (s *secretConnection) call(path dbus.ObjectPath, method string, args ...interface{}) *dbus.Call {
	return s.conn.Object(secretDestination, path).CallWithContext(s.ctx, method, 0, args...)
}

func (s *secretConnection) prompt(path dbus.ObjectPath) (dbus.Variant, error) {
	if path == "/" {
		return dbus.MakeVariant(""), nil
	}
	if !path.IsValid() {
		return dbus.Variant{}, ErrUnavailable
	}
	options := []dbus.MatchOption{dbus.WithMatchSender(secretDestination), dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(secretPrompt), dbus.WithMatchMember("Completed")}
	if err := s.conn.AddMatchSignalContext(s.ctx, options...); err != nil {
		return dbus.Variant{}, ErrUnavailable
	}
	signals := make(chan *dbus.Signal, 1)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)
	// Closing this dedicated connection removes its match and session.
	if err := s.call(path, secretPrompt+".Prompt", "").Err; err != nil {
		return dbus.Variant{}, ErrUnavailable
	}
	select {
	case signal := <-signals:
		if signal == nil || signal.Path != path || signal.Name != secretPrompt+".Completed" || len(signal.Body) != 2 {
			return dbus.Variant{}, ErrUnavailable
		}
		dismissed, ok := signal.Body[0].(bool)
		if !ok || dismissed {
			return dbus.Variant{}, ErrUnavailable
		}
		result, ok := signal.Body[1].(dbus.Variant)
		if !ok {
			return dbus.Variant{}, ErrUnavailable
		}
		return result, nil
	case <-s.ctx.Done():
		return dbus.Variant{}, ErrUnavailable
	}
}

func (s *secretConnection) unlock(path dbus.ObjectPath) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := s.call(secretPath, secretService+".Unlock", []dbus.ObjectPath{path}).Store(&unlocked, &prompt); err != nil {
		return ErrUnavailable
	}
	result, err := s.prompt(prompt)
	if err != nil {
		return err
	}
	if paths, ok := result.Value().([]dbus.ObjectPath); ok {
		unlocked = append(unlocked, paths...)
	}
	for _, candidate := range unlocked {
		if candidate == path {
			return nil
		}
	}
	return ErrUnavailable
}

func credentialAttributes(key string) map[string]string {
	return map[string]string{"service": serviceName, "account": key}
}

func (s *secretConnection) find(key string) (dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	if err := s.call(secretPath, secretService+".SearchItems", credentialAttributes(key)).Store(&unlocked, &locked); err != nil {
		return "", ErrUnavailable
	}
	items := append(unlocked, locked...)
	if len(items) == 0 {
		return "", ErrNotFound
	}
	if len(items) != 1 || !items[0].IsValid() || items[0] == "/" {
		return "", ErrUnavailable
	}
	if len(locked) > 0 {
		if err := s.unlock(items[0]); err != nil {
			return "", err
		}
	}
	return items[0], nil
}

func (s *secretConnection) session() (dbus.ObjectPath, error) {
	var output dbus.Variant
	var path dbus.ObjectPath
	if err := s.call(secretPath, secretService+".OpenSession", "plain", dbus.MakeVariant("")).Store(&output, &path); err != nil {
		return "", ErrUnavailable
	}
	if !path.IsValid() || path == "/" {
		return "", ErrUnavailable
	}
	return path, nil
}

func (linuxStore) Get(key string) (string, error) {
	s, err := connectSecretService()
	if err != nil {
		return "", err
	}
	defer s.close()
	item, err := s.find(key)
	if err != nil {
		return "", err
	}
	session, err := s.session()
	if err != nil {
		return "", err
	}
	var secret linuxSecret
	if err := s.call(item, secretItem+".GetSecret", session).Store(&secret); err != nil {
		return "", ErrUnavailable
	}
	defer clear(secret.Value)
	if secret.Session != session || len(secret.Parameters) != 0 {
		return "", ErrUnavailable
	}
	return string(secret.Value), nil
}

func (linuxStore) Set(key, value string) error {
	s, err := connectSecretService()
	if err != nil {
		return err
	}
	defer s.close()
	item, err := s.find(key)
	if err != nil && err != ErrNotFound {
		return err
	}
	missing := err == ErrNotFound
	session, err := s.session()
	if err != nil {
		return err
	}
	secret := linuxSecret{Session: session, Parameters: []byte{}, Value: []byte(value), ContentType: "text/plain; charset=utf-8"}
	defer clear(secret.Value)
	if !missing {
		return normalizeError(s.call(item, secretItem+".SetSecret", secret).Err)
	}
	var collection dbus.ObjectPath
	if err = s.call(secretPath, secretService+".ReadAlias", "default").Store(&collection); err != nil || !collection.IsValid() || collection == "/" {
		return ErrUnavailable
	}
	if err = s.unlock(collection); err != nil {
		return err
	}
	properties := map[string]dbus.Variant{
		secretItem + ".Label":      dbus.MakeVariant("LedgeSync Google Drive authorization"),
		secretItem + ".Attributes": dbus.MakeVariant(credentialAttributes(key)),
	}
	var prompt dbus.ObjectPath
	if err = s.call(collection, secretCollection+".CreateItem", properties, secret, true).Store(&item, &prompt); err != nil {
		return ErrUnavailable
	}
	result, err := s.prompt(prompt)
	if err != nil {
		return err
	}
	if prompt != "/" {
		var ok bool
		item, ok = result.Value().(dbus.ObjectPath)
		if !ok {
			return ErrUnavailable
		}
	}
	if !item.IsValid() || item == "/" {
		return ErrUnavailable
	}
	return nil
}

func (linuxStore) Delete(key string) error {
	s, err := connectSecretService()
	if err != nil {
		return err
	}
	defer s.close()
	item, err := s.find(key)
	if err != nil {
		return err
	}
	var prompt dbus.ObjectPath
	if err := s.call(item, secretItem+".Delete").Store(&prompt); err != nil {
		return ErrUnavailable
	}
	_, err = s.prompt(prompt)
	return err
}
