"use strict";

const https = require("https");
const http = require("http");
const fs = require("fs");
const path = require("path");

const VERSION = require("./package.json").version;
const REPO = "jazztong/remote-terminal";

const PLATFORM_MAP = {
  "linux-x64": "remote-term-linux-amd64",
  "darwin-x64": "remote-term-darwin-amd64",
  "darwin-arm64": "remote-term-darwin-arm64",
  "win32-x64": "remote-term-windows-amd64.exe",
};

function getBinaryName() {
  const key = `${process.platform}-${process.arch}`;
  const name = PLATFORM_MAP[key];
  if (!name) {
    console.error(
      `Unsupported platform: ${key}\n` +
        `Supported: ${Object.keys(PLATFORM_MAP).join(", ")}\n` +
        `Please build from source: https://github.com/${REPO}`
    );
    process.exit(1);
  }
  return name;
}

function getDownloadUrl(binaryName) {
  return `https://github.com/${REPO}/releases/download/v${VERSION}/${binaryName}`;
}

function download(url) {
  return new Promise((resolve, reject) => {
    const get = url.startsWith("https") ? https.get : http.get;
    get(url, (res) => {
      if (res.statusCode === 301 || res.statusCode === 302) {
        download(res.headers.location).then(resolve).catch(reject);
        return;
      }

      if (res.statusCode !== 200) {
        reject(new Error(`Download failed: HTTP ${res.statusCode} from ${url}`));
        return;
      }

      const chunks = [];
      res.on("data", (chunk) => chunks.push(chunk));
      res.on("end", () => resolve(Buffer.concat(chunks)));
      res.on("error", reject);
    }).on("error", reject);
  });
}

async function main() {
  const binaryName = getBinaryName();
  const url = getDownloadUrl(binaryName);
  const binDir = path.join(__dirname, "bin");
  const isWindows = process.platform === "win32";
  const outputName = isWindows ? "remote-term-binary.exe" : "remote-term-binary";
  const outputPath = path.join(binDir, outputName);

  console.log(`Downloading remote-term v${VERSION} for ${process.platform}-${process.arch}...`);

  try {
    const data = await download(url);
    fs.mkdirSync(binDir, { recursive: true });
    fs.writeFileSync(outputPath, data);

    if (!isWindows) {
      fs.chmodSync(outputPath, 0o755);
    }

    console.log(`Installed remote-term v${VERSION} to ${outputPath}`);
  } catch (err) {
    console.error(`Failed to download remote-term: ${err.message}`);
    console.error(`\nManual install:`);
    console.error(`  Download: ${url}`);
    console.error(`  Place in: ${binDir}`);
    process.exit(1);
  }
}

main();
