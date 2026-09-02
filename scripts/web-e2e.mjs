import assert from "node:assert/strict";
import process from "node:process";

const playwrightModule = process.env.RESTOREWEAVE_PLAYWRIGHT_MODULE || "playwright";
const { chromium } = await import(playwrightModule);

const phase = process.argv[2] ?? "online";
const url = process.env.RESTOREWEAVE_WEB_E2E_URL ?? "http://127.0.0.1:5174/";
const sourcePath = process.env.RESTOREWEAVE_WEB_E2E_SOURCE;

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
const button = (name) => page.getByRole("button", { name });
const text = (pattern) => page.getByText(pattern);

try {
  await page.goto(url, { waitUntil: "networkidle" });
  if (phase === "offline") {
    await assertOffline();
  } else if (phase === "recovered") {
    await assertRecovered();
  } else {
    assert(sourcePath, "RESTOREWEAVE_WEB_E2E_SOURCE is required for online phase");
    await assertOnline();
  }
} finally {
  await browser.close();
}

async function assertOffline() {
  await page.getByRole("button", { name: /刷新服务状态|Refresh service status/ }).click();
  await text(/服务不可用|service is unavailable/i).waitFor({ state: "visible", timeout: 10000 });
  assert.equal(await page.getByRole("textbox", { name: /搜索内容库|Search library/ }).isDisabled(), true);
}

async function assertOnline() {
  await text(/已连接|Connected/).waitFor({ state: "visible", timeout: 10000 });
  await button(/添加来源|Add source/).first().click();
  const pathInput = page.getByRole("textbox", { name: /RestoreWeave 主机上的路径|Path on RestoreWeave host/ });
  await pathInput.fill(sourcePath);
  await button(/预览存储计划|Preview storage plan/).click();
  await text(/存储计划已生成|Storage plan is ready/).waitFor({ state: "visible", timeout: 10000 });
  await button(/保存原始副本|Save exact copy/).click();
  await text(/内容已保存|Content saved/).waitFor({ state: "visible", timeout: 15000 });

  const item = button(/browser-e2e\.txt/);
  await item.waitFor({ state: "visible", timeout: 10000 });
  await item.click();
  await page.getByRole("combobox", { name: /新标签|New tag/ }).fill("browser-e2e");
  await button(/添加标签|Add tag/).click();
  await page.getByRole("textbox", { name: /新备注|New note/ }).fill("durable browser note");
  await button(/添加备注|Add note/).click();
  await text("durable browser note").waitFor({ state: "visible", timeout: 10000 });
  await text("browser-e2e").waitFor({ state: "visible", timeout: 10000 });

  const search = page.getByRole("textbox", { name: /搜索内容库|Search library/ });
  await search.fill("durable browser note");
  await search.press("Enter");
  await text(/搜索结果|Search results/).waitFor({ state: "visible", timeout: 15000 });
  await text("durable browser note").waitFor({ state: "visible", timeout: 10000 });
  await text(/部分搜索覆盖已降级|search coverage.*degraded/i).waitFor({ state: "visible", timeout: 10000 });
  await assertNoHorizontalOverflow();
}

async function assertRecovered() {
  await page.getByRole("button", { name: /刷新服务状态|Refresh service status/ }).click();
  await text(/已连接|Connected/).waitFor({ state: "visible", timeout: 10000 });
  await text("browser-e2e.txt").waitFor({ state: "visible", timeout: 10000 });
  await text("browser-e2e").waitFor({ state: "visible", timeout: 10000 });
  await page.getByRole("button", { name: /browser-e2e\.txt/ }).click();
  await text("durable browser note").waitFor({ state: "visible", timeout: 10000 });
  await assertNoHorizontalOverflow();
}

async function assertNoHorizontalOverflow() {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(150);
  const dimensions = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    bodyScrollWidth: document.body.scrollWidth,
  }));
  assert.ok(dimensions.scrollWidth <= dimensions.clientWidth, `horizontal overflow: ${JSON.stringify(dimensions)}`);
  assert.ok(dimensions.bodyScrollWidth <= dimensions.clientWidth, `body horizontal overflow: ${JSON.stringify(dimensions)}`);
}
