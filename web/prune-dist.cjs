// Deletes built assets that index.html no longer references. Vite does not
// empty an outDir outside the project root reliably here, and stale bundles
// would otherwise be committed and embedded.
const fs = require('fs');
const path = require('path');
const dist = path.resolve(__dirname, '../internal/httpd/ui/dist');
const index = fs.readFileSync(path.join(dist, 'index.html'), 'utf8');
const assets = path.join(dist, 'assets');
for (const name of fs.readdirSync(assets)) {
  if (!index.includes(`assets/${name}`)) {
    fs.unlinkSync(path.join(assets, name));
    console.log('removed stale', name);
  }
}
