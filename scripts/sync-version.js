const fs = require('fs');
const path = require('path');

const pkgPath = path.join(__dirname, '../package.json');
const versionGoPath = path.join(__dirname, '../internal/config/version.go');

const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));
const version = pkg.version;

if (!version) {
  console.error('Error: No version found in package.json');
  process.exit(1);
}

const goContent = `package config

// AppVersion is the current JellyGate release version.
const AppVersion = "${version}"
`;

fs.writeFileSync(versionGoPath, goContent, 'utf8');
console.log(`Synced version ${version} to internal/config/version.go`);
