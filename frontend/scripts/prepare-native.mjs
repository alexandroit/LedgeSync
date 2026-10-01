// Deterministically rasterize the application-owned vector mark without native dependencies.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve, dirname } from 'node:path';
import { deflateSync } from 'node:zlib';

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const build = resolve(frontend, '../build');
const shape = JSON.parse(readFileSync(resolve(frontend, 'native/mark.json'), 'utf8'));
const { size, inset, radius, background, stroke, strokeWidth, points } = shape;
const pixels = Buffer.alloc(size * (size * 4 + 1));
function segmentDistance(x, y, a, b) {
  const dx = b[0] - a[0], dy = b[1] - a[1];
  const t = Math.max(0, Math.min(1, ((x - a[0]) * dx + (y - a[1]) * dy) / (dx * dx + dy * dy)));
  return (x - a[0] - t * dx) ** 2 + (y - a[1] - t * dy) ** 2;
}
for (let y = 0; y < size; y++) {
  for (let x = 0; x < size; x++) {
    const rgba = [0, 0, 0, 0];
    for (const sy of [.25, .75]) for (const sx of [.25, .75]) {
      const px = x + sx, py = y + sy;
      const dx = Math.max(inset + radius - px, 0, px - (size - inset - radius));
      const dy = Math.max(inset + radius - py, 0, py - (size - inset - radius));
      if (dx * dx + dy * dy > radius * radius) continue;
      const onStroke = points.slice(1).some((point, i) => segmentDistance(px, py, points[i], point) <= (strokeWidth / 2) ** 2);
      const color = onStroke ? stroke : background;
      for (let c = 0; c < 3; c++) rgba[c] += color[c];
      rgba[3] += 255;
    }
    const offset = y * (size * 4 + 1) + 1 + x * 4;
    const samples = rgba[3] / 255;
    for (let c = 0; c < 3; c++) pixels[offset + c] = samples ? Math.round(rgba[c] / samples) : 0;
    pixels[offset + 3] = Math.round(rgba[3] / 4);
  }
}
const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c >>> 0;
});
function chunk(name, data) {
  const type = Buffer.from(name), length = Buffer.alloc(4), crc = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  let check = 0xffffffff;
  for (const b of Buffer.concat([type, data])) check = crcTable[(check ^ b) & 255] ^ (check >>> 8);
  crc.writeUInt32BE((check ^ 0xffffffff) >>> 0);
  return Buffer.concat([length, type, data, crc]);
}
const header = Buffer.alloc(13); header.writeUInt32BE(size, 0); header.writeUInt32BE(size, 4); header[8] = 8; header[9] = 6;
const png = Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', header), chunk('IDAT', deflateSync(pixels, { level: 9 })), chunk('IEND', Buffer.alloc(0))]);
mkdirSync(resolve(build, 'darwin'), { recursive: true });
writeFileSync(resolve(build, 'appicon.png'), png);
const plist = readFileSync(resolve(frontend, 'native/Info.plist'), 'utf8');
writeFileSync(resolve(build, 'darwin/Info.plist'), plist);
writeFileSync(resolve(build, 'darwin/Info.dev.plist'), plist.replace('com.ledgesync.app', 'com.ledgesync.app.dev').replace('</dict>', '<key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict></dict>'));
console.log('Prepared LedgeSync native icon and macOS 13 bundle identity.');
