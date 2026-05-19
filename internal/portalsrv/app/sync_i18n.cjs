const fs = require("fs");
const path = require("path");

const i18nDir = path.join(__dirname, "src/i18n");
const enPath = path.join(i18nDir, "en.json");

if (!fs.existsSync(enPath)) {
  console.error("en.json not found at", enPath);
  process.exit(1);
}

const en = JSON.parse(fs.readFileSync(enPath, "utf8"));

function deepSync(source, target) {
  const result = {};
  for (const key of Object.keys(source)) {
    const sVal = source[key];
    const tVal = target ? target[key] : undefined;

    if (sVal && typeof sVal === "object" && !Array.isArray(sVal)) {
      result[key] = deepSync(sVal, tVal);
    } else {
      result[key] = tVal !== undefined ? tVal : sVal;
    }
  }
  return result;
}

fs.readdirSync(i18nDir).forEach((file) => {
  if (file === "en.json" || !file.endsWith(".json")) return;

  const filePath = path.join(i18nDir, file);
  try {
    const content = JSON.parse(fs.readFileSync(filePath, "utf8"));
    const synced = deepSync(en, content);
    fs.writeFileSync(filePath, JSON.stringify(synced, null, 2) + "\n");
    console.log("Synced " + file);
  } catch (err) {
    console.error("Error syncing " + file + ":", err);
  }
});
