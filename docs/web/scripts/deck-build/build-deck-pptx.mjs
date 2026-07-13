#!/usr/bin/env node
/*
 * build-deck-pptx.mjs — reproducible build of docs/DEMO_ASDLC_DECK.pptx
 *
 * Pipeline:
 *   1. Read docs/DEMO_ASDLC_DECK.md (the committed source; keeps ```mermaid fences).
 *   2. Render every ```mermaid block to a PNG via @mermaid-js/mermaid-cli (mmdc).
 *   3. Substitute each fence with an image reference in a TEMP markdown copy.
 *   4. Run @marp-team/marp-cli --pptx --allow-local-files to emit the .pptx.
 *
 * The committed .md is never modified — only a temp copy under build/ is rewritten.
 *
 * Run:
 *   cd docs/web/scripts/deck-build
 *   npm install            # one-time (pins mmdc + marp-cli)
 *   npm run build          # regenerates ../../../DEMO_ASDLC_DECK.pptx
 *
 * Requirements: a Chrome/Chromium for Puppeteer. mmdc downloads one on install;
 * marp-cli auto-detects an installed Chrome, or honor CHROME_PATH / $PUPPETEER_EXECUTABLE_PATH.
 */
import { execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync, rmSync, existsSync } from 'node:fs';
import { dirname, resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, '../../../..');
const deckMd = join(repoRoot, 'docs/DEMO_ASDLC_DECK.md');
const outPptx = join(repoRoot, 'docs/DEMO_ASDLC_DECK.pptx');
const buildDir = join(__dirname, 'build');
const imgDir = join(buildDir, 'img');
const tmpMd = join(buildDir, 'deck.tmp.md');

const binDir = join(__dirname, 'node_modules', '.bin');
const mmdc = join(binDir, process.platform === 'win32' ? 'mmdc.cmd' : 'mmdc');
const marp = join(binDir, process.platform === 'win32' ? 'marp.cmd' : 'marp');

// Marp slide canvas is 1280x720 with overflow:hidden — anything taller is clipped.
// Render mermaid at a high scale for crisp PNGs; Marp CSS (in the deck frontmatter)
// constrains the displayed height so diagrams fit the fold.
const MMDC_SCALE = '2';
const MMDC_WIDTH = '1400';
const MMDC_THEME = 'default';
const MMDC_BG = 'white';

// Resolve a Chrome/Chromium for Puppeteer (mmdc) and Marp. Honor env first,
// then fall back to common install locations.
function resolveChrome() {
  const candidates = [
    process.env.PUPPETEER_EXECUTABLE_PATH,
    process.env.CHROME_PATH,
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
  ].filter(Boolean);
  return candidates.find((p) => existsSync(p)) || null;
}

function sh(bin, args, opts = {}) {
  return execFileSync(bin, args, { stdio: 'inherit', cwd: __dirname, ...opts });
}

function main() {
  if (!existsSync(mmdc) || !existsSync(marp)) {
    console.error('Missing tooling. Run `npm install` in docs/web/scripts/deck-build first.');
    process.exit(1);
  }
  rmSync(buildDir, { recursive: true, force: true });
  mkdirSync(imgDir, { recursive: true });

  const chrome = resolveChrome();
  const marpEnv = { ...process.env };
  let puppeteerCfg = null;
  if (chrome) {
    puppeteerCfg = join(buildDir, 'puppeteer.json');
    writeFileSync(puppeteerCfg, JSON.stringify({ executablePath: chrome, args: ['--no-sandbox'] }));
    marpEnv.CHROME_PATH = chrome;
    console.log(`Using Chrome: ${chrome}`);
  } else {
    console.log('No system Chrome found; relying on Puppeteer-managed browser.');
  }

  const src = readFileSync(deckMd, 'utf8');

  // Split out fenced mermaid blocks, render each, and replace with an <img> ref.
  const fence = /```mermaid\n([\s\S]*?)\n```/g;
  let idx = 0;
  const out = src.replace(fence, (_m, code) => {
    const i = idx++;
    const mmd = join(imgDir, `d${i}.mmd`);
    const png = join(imgDir, `d${i}.png`);
    writeFileSync(mmd, code, 'utf8');
    sh(mmdc, [
      '-i', mmd,
      '-o', png,
      '-b', MMDC_BG,
      '-t', MMDC_THEME,
      '-s', MMDC_SCALE,
      '-w', MMDC_WIDTH,
      '--quiet',
      ...(puppeteerCfg ? ['-p', puppeteerCfg] : []),
    ]);
    // class=mermaid lets the deck CSS cap each diagram's height to the fold.
    return `![mermaid](${png})`;
  });
  console.log(`Rendered ${idx} mermaid diagram(s).`);

  writeFileSync(tmpMd, out, 'utf8');

  sh(marp, [
    tmpMd,
    '--pptx',
    '--allow-local-files',
    '-o', outPptx,
  ], { env: marpEnv });
  console.log(`Wrote ${outPptx}`);
}

main();
