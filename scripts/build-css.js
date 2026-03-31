const fs = require('node:fs');
const path = require('node:path');

// Build Tailwind using PostCSS programmatically. This avoids relying on a
// specific CLI file path which changed between Tailwind major versions.
async function build() {
  const postcss = require('postcss');
  const tailwindcss = require('@tailwindcss/postcss');

  const inputPath = path.resolve(__dirname, '..', 'web', 'static', 'css', 'tailwind.input.css');
  const outputPath = path.resolve(__dirname, '..', 'web', 'static', 'css', 'tailwind.generated.css');
  const configPath = path.resolve(__dirname, '..', 'tailwind.config.js');

  const input = await fs.promises.readFile(inputPath, 'utf8');
  // In Tailwind 4, the config is discovered via @import "tailwindcss" and @config in the CSS file.
  const result = await postcss([tailwindcss()]).process(input, { from: inputPath, to: outputPath });

  await fs.promises.mkdir(path.dirname(outputPath), { recursive: true });
  await fs.promises.writeFile(outputPath, result.css, 'utf8');
  if (result.map) {
    await fs.promises.writeFile(outputPath + '.map', result.map.toString(), 'utf8');
  }
}

build().then(() => process.exit(0)).catch(err => {
  console.error(err);
  process.exit(1);
});