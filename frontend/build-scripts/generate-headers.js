import fs from 'fs';
import path from 'path';
import { parse } from 'node-html-parser';

const __dirname = path.resolve();
const buildDir = path.join(__dirname, 'build');

function removeCspMeta(inputFile) {
    const fileContents = fs.readFileSync(inputFile, { encoding: 'utf-8' });
    const root = parse(fileContents);
    const element = root.querySelector('head meta[http-equiv="content-security-policy"]');
    const content = element.getAttribute('content');
    element.remove()
    return content;
}

const cspMap = new Map();

function findCspMeta(startPath, filter = /\.html$/) {
    if (!fs.existsSync(startPath)) {
        console.error(`Unable to find CSP start path: ${startPath}`);
        return;
    }
    const files = fs.readdirSync(startPath);
    files.forEach((item) => {
        const filename = path.join(startPath, item);
        const stat = fs.lstatSync(filename);
        if (stat.isDirectory()) {
            findCspMeta(filename, filter);
        } else if (filter.test(filename)) {
            cspMap.set(
                filename
                    .replace(buildDir, '')
                    .replace(/\.html$/, '')
                    .replace(/^\/index$/, '/'),
                removeCspMeta(filename),
            );
        }
    });
}

function createHeaders() {
    const headers = `  Cross-Origin-Embedder-Policy: require-corp
  Cross-Origin-Opener-Policy: same-origin
  Strict-Transport-Security: max-age=63072000; includeSubDomains; preload
  Referrer-Policy: no-referrer
  X-Content-Type-Options: nosniff
  X-Frame-Options: DENY
  X-Permitted-Cross-Domain-Policies: 'none'
`;
    const cspHashes = {};
    cspMap.forEach((csp) => {
        const cspParts = csp.split(" ")
        if (cspParts.length != 3) {
            console.error(`CSP had unexpected number of parts, expected 3: ${csp}`)
            throw new Error("failed to collect hashes for inline CSP")
        }
        // push the key, so we only have the unique hashes
        cspHashes[cspParts[2]] = true
    })
    const cspHashesInline = Object.keys(cspHashes).join(" ")
    const csp = (`  Content-Security-Policy: script-src 'self' ${cspHashesInline}`)

    const headersFile = path.join(buildDir, '_headers');
    const allHeaders = `/*\n${csp}\n${headers}\n`
    fs.writeFileSync(headersFile, allHeaders);
    return allHeaders
}

async function main() {
    findCspMeta(buildDir);
    return createHeaders();
}

const headers = await main();
console.log("generate-headers: Success: Removed CSP from html and added _headers file")
