const { spawnSync } = require('node:child_process');
const path = require('node:path');

const tailwindCLI = path.resolve(__dirname, '..', 'node_modules', 'tailwindcss', 'lib', 'cli.js');
const args = [
    tailwindCLI,
    '-i', './web/static/css/tailwind.input.css',
    '-c', './tailwind.config.js',
    '-o', './web/static/css/tailwind.generated.css',
    '--minify',
];

const result = spawnSync(process.execPath, args, {
    stdio: 'inherit',
    env: {
        ...process.env,
        BROWSERSLIST_IGNORE_OLD_DATA: '1',
    },
});

if (result.error) {
    throw result.error;
}

process.exit(result.status ?? 1);