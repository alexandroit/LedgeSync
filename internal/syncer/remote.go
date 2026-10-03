package syncer

import (
	"context"
	"io"
	"sort"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
)

// Remote is the Drive port used by sync; *drive.Client implements it.
type Remote interface {
	GetObject(ctx context.Context, account, id string) (drive.Object, error)
	GetFolder(ctx context.Context, account, id string) (drive.Folder, error)
	ListChildren(ctx context.Context, account, parent string) ([]drive.Object, error)
	GenerateIDs(ctx context.Context, account string, count int) ([]string, error)
	CreateFolder(ctx context.Context, account, id, parent, name, operation string) (drive.Object, error)
	Upload(ctx context.Context, account, id, parent, name, operation string, source io.ReadSeeker, size int64, md5 string) (drive.Object, error)
	UpdateContent(ctx context.Context, account, id string, source io.ReadSeeker, size int64, md5 string) (drive.Object, error)
	Trash(ctx context.Context, account, id string) error
	Download(ctx context.Context, account, id string, size int64, w io.Writer) error
	StartPageToken(ctx context.Context, account string) (string, error)
	Changes(ctx context.Context, account, token string) ([]drive.Change, string, error)
}

type remoteItem struct {
	ID       string
	Kind     string // "file" or "dir"
	Size     int64
	MD5      string
	Version  string
	Modified string
	Parent   string
}

func remoteFrom(o drive.Object, parent string) remoteItem {
	item := remoteItem{ID: o.ID, Kind: "file", Size: o.Size, MD5: o.MD5, Version: o.Version, Modified: o.ModifiedTime, Parent: parent}
	if drive.IsFolder(o) {
		item.Kind, item.Size, item.MD5 = "dir", 0, ""
	}
	return item
}

// scanRemote lists the synced Drive folder recursively. Items that cannot be
// synced (Google-native files, duplicate names, names this system cannot
// store) are reported and left untouched; excluded paths are skipped.
func scanRemote(ctx context.Context, r Remote, account, rootID string, excluded func(p, kind string) (bool, error)) (map[string]remoteItem, []Issue, error) {
	items := map[string]remoteItem{}
	var issues []Issue
	type folder struct{ id, path string }
	queue := []folder{{rootID, ""}}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		current := queue[0]
		queue = queue[1:]
		children, err := r.ListChildren(ctx, account, current.id)
		if err != nil {
			return nil, nil, err
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
		exact := map[string]int{}
		folded := map[string]int{}
		for _, c := range children {
			exact[c.Name]++
			folded[foldKey(c.Name)]++
		}
		for _, c := range children {
			p := c.Name
			if current.path != "" {
				p = current.path + "/" + c.Name
			}
			if builtinExcluded(c.Name) {
				continue
			}
			kind := "file"
			if drive.IsFolder(c) {
				kind = "dir"
			}
			skip, err := excluded(p, kind)
			if err != nil {
				return nil, nil, err
			}
			switch {
			case skip:
				continue
			case drive.GoogleNative(c.MimeType):
				issues = append(issues, Issue{p, "GOOGLE_FILE_SKIPPED", "Google Docs, Sheets, Slides and shortcuts stay in Drive and are not downloaded."})
				continue
			case exact[c.Name] > 1 || folded[foldKey(c.Name)] > 1:
				issues = append(issues, Issue{p, "DUPLICATE_NAME", "Google Drive has more than one item with this name here; rename one in Drive to sync it."})
				continue
			case !localName(c.Name):
				issues = append(issues, Issue{p, "NAME_UNSUPPORTED", "This name cannot be used for a file on this computer; rename it in Drive to sync it."})
				continue
			}
			items[p] = remoteFrom(c, current.id)
			if len(items) > maxRemoteItems {
				return nil, nil, domain.Fail("SYNC_TOO_LARGE", "The Drive folder has more items than LedgeSync syncs in one folder.")
			}
			if kind == "dir" {
				queue = append(queue, folder{c.ID, p})
			}
		}
	}
	return items, issues, nil
}
