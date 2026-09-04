import assert from "node:assert/strict";
import process from "node:process";

const playwrightModule = process.env.RESTOREWEAVE_PLAYWRIGHT_MODULE || "playwright";
const { chromium } = await import(playwrightModule);

const phase = process.argv[2] ?? "online";
const url = process.env.RESTOREWEAVE_WEB_E2E_URL ?? "http://127.0.0.1:5174/";
const sourcePath = process.env.RESTOREWEAVE_WEB_E2E_SOURCE;
const restoreDestination = process.env.RESTOREWEAVE_WEB_E2E_RESTORE_DEST;

// CI and developer machines may already provide a managed browser (for
// example, the system Google Chrome) without a Playwright-downloaded binary.
// Keep the default unchanged, but allow the harness to point at that browser
// explicitly so a missing cache does not turn a real E2E run into a false skip.
const executablePath = process.env.RESTOREWEAVE_PLAYWRIGHT_EXECUTABLE_PATH?.trim();
const browser = await chromium.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
const button = (name) => page.getByRole("button", { name });
const text = (pattern) => page.getByText(pattern);

try {
  if (phase === "offline") {
    await page.route("**/api/v1/**", (route) => route.abort());
  }
  await page.goto(url, { waitUntil: "networkidle" });
  if (phase === "offline") {
    await assertOffline();
  } else if (phase === "recovered") {
    await assertRecovered();
  } else {
    assert(sourcePath, "RESTOREWEAVE_WEB_E2E_SOURCE is required for online phase");
    assert(restoreDestination, "RESTOREWEAVE_WEB_E2E_RESTORE_DEST is required for online phase");
    await assertOnline();
  }
} finally {
  await browser.close();
}

async function assertOffline() {
  await page.reload({ waitUntil: "networkidle" });
  await page.getByRole("button", { name: /刷新服务状态|Refresh service status/ }).click();
  await text(/服务不可用|service is unavailable/i).waitFor({ state: "visible", timeout: 10000 });
  assert.equal(await page.getByRole("textbox", { name: /搜索内容库|Search library/ }).isDisabled(), true);
}

async function assertOnline() {
  await text(/已连接|Connected/).waitFor({ state: "visible", timeout: 10000 });
  await button(/添加来源|Add source/).first().click();
  const addDrawer = page.getByRole("dialog", { name: /添加来源|Add source/ });
  await addDrawer.waitFor({ state: "visible", timeout: 10000 });
  const pathInput = addDrawer.getByRole("textbox", { name: /RestoreWeave 主机上的路径|Path on RestoreWeave host/ });
  await pathInput.fill(sourcePath);
  await addDrawer.getByRole("button", { name: /预览存储计划|Preview storage plan/ }).click();
  await addDrawer.getByText(/存储计划已生成|Storage plan is ready/).waitFor({ state: "visible", timeout: 10000 });
  const planStats = await addDrawer.locator(".plan-stats").first().innerText();
  assert.match(planStats, /2\s*(?:files?|文件(?:数)?)/i, `expected two source files: ${planStats}`);
  assert.match(planStats, /(duplicate bytes expected to reuse|重复内容)/i, `missing duplicate facet: ${planStats}`);
  // Human-readable byte values may be fractional (for example, 20.4 KB).
  // Accept those values so the assertion checks the presence of the distinct
  // logical/physical figures without coupling the test to formatting.
  const planNumbers = planStats.match(/\b[1-9][0-9]*(?:\.[0-9]+)?\s*(B|KB|MB|GB|字节)\b/g) ?? [];
  assert.ok(planNumbers.length >= 2, `expected unique and duplicate byte figures: ${planStats}`);
  await addDrawer.getByRole("button", { name: /保存原始副本|Save exact copy/ }).click();
  await page.getByRole("main").getByText(/内容已保存|Content saved/).waitFor({ state: "visible", timeout: 15000 });
  // The applied plan remains open so the operator can inspect measured
  // deduplication/physical savings. Close it explicitly before continuing
  // with the library actions that sit behind the drawer.
  await addDrawer.getByRole("button", { name: /关闭添加来源|Close add source/ }).click();

  const item = button(/alpha\.txt/);
  await item.waitFor({ state: "visible", timeout: 10000 });
  await item.click();
  await page.getByRole("combobox", { name: /新标签|New tag/ }).fill("browser-e2e");
  await button(/添加标签|Add tag/).click();
  await page.getByRole("button", { name: /移除标签 browser-e2e|Remove tag browser-e2e/ }).waitFor({ state: "visible", timeout: 10000 });
  await page.getByRole("textbox", { name: /新备注|New note/ }).fill("durable browser note");
  await button(/添加备注|Add note/).click();
  await page.getByText("durable browser note", { exact: true }).waitFor({ state: "visible", timeout: 10000 });
  await page.getByRole("complementary").getByText("browser-e2e", { exact: true }).waitFor({ state: "visible", timeout: 10000 });

  await button(/恢复最新快照|Restore latest snapshot/).click();
  await page.getByRole("textbox", { name: /目标文件夹|Destination folder/ }).fill(restoreDestination);
  await button(/预览恢复计划|Preview restore/).click();
  await text(/恢复计划已生成|Restore plan is ready/).waitFor({ state: "visible", timeout: 10000 });
  await button(/确认恢复|Confirm restore/).click();
  // Exact restore is allowed to report a truthful DEGRADED result when a
  // non-essential fidelity/metadata check is unavailable. Both outcomes
  // still prove that the restore operation completed and exposed its state.
  await text(/已恢复(?:并校验|[^。]*但部分完整性或元数据检查不可用)|Restored(?: and verified| [^.]*(?:but some fidelity or metadata checks were unavailable))/i).waitFor({ state: "visible", timeout: 15000 });

  const search = page.getByRole("textbox", { name: /搜索内容库|Search library/ });
  await search.fill("durable browser note");
  await search.press("Enter");
  await page.getByRole("main").getByRole("heading", { name: /搜索结果|Results for "/i }).waitFor({ state: "visible", timeout: 15000 });
  await page.locator(".result-match").filter({ hasText: "durable browser note" }).first().waitFor({ state: "visible", timeout: 10000 });
  // A healthy semantic provider does not need to emit a degradation notice;
  // when it does, ensure the notice is visible rather than requiring one in
  // every environment.
  const degradedSearch = text(/部分搜索覆盖已降级|search coverage.*degraded/i);
  if (await degradedSearch.count() > 0) {
    await degradedSearch.first().waitFor({ state: "visible", timeout: 10000 });
  }
  await assertNarrowDrawers();
}

async function assertRecovered() {
  await page.getByRole("button", { name: /刷新服务状态|Refresh service status/ }).click();
  await text(/已连接|Connected/).waitFor({ state: "visible", timeout: 10000 });
  const recoveredItem = page.getByRole("main").getByRole("button", { name: /alpha\.txt/ }).first();
  await recoveredItem.waitFor({ state: "visible", timeout: 10000 });
  await page.getByRole("main").getByText("browser-e2e", { exact: true }).first().waitFor({ state: "visible", timeout: 10000 });
  await recoveredItem.click();
  await page.getByText("durable browser note", { exact: true }).waitFor({ state: "visible", timeout: 10000 });
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

async function assertNarrowDrawers() {
  // Exercise each primary drawer at the narrowest supported layout. This
  // catches controls that are visually present but unreachable on mobile.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(150);

  const settingsTrigger = page.getByRole("button", { name: /打开设置|Open settings/ });
  await settingsTrigger.click();
  const settings = page.getByRole("dialog", { name: /设置|Settings/ });
  await settings.waitFor({ state: "visible", timeout: 10000 });
  assert.ok(await settings.getByRole("button", { name: /关闭设置|Close settings/ }).isVisible());
  await assertNoHorizontalOverflow();
  await settings.getByRole("button", { name: /关闭设置|Close settings/ }).click();
  await assertFocusReturned(settingsTrigger);

  const addTrigger = page.getByRole("button", { name: /添加来源|Add source/ }).first();
  await addTrigger.click();
  const addDrawer = page.getByRole("dialog", { name: /添加来源|Add source/ });
  await addDrawer.waitFor({ state: "visible", timeout: 10000 });
  assert.ok(await addDrawer.getByRole("textbox", { name: /路径|Path on RestoreWeave host/ }).isVisible());
  await assertNoHorizontalOverflow();
  await addDrawer.getByRole("button", { name: /关闭添加来源|Close add source/ }).click();
  await assertFocusReturned(addTrigger);

  const restoreTrigger = page.getByRole("button", { name: /恢复最新快照|Restore latest snapshot/ });
  await restoreTrigger.click();
  const restoreDrawer = page.getByRole("dialog", { name: /恢复快照|Restore snapshot/ });
  await restoreDrawer.waitFor({ state: "visible", timeout: 10000 });
  assert.ok(await restoreDrawer.getByRole("textbox", { name: /目标文件夹|Destination folder/ }).isVisible());
  await assertNoHorizontalOverflow();
  await restoreDrawer.getByRole("button", { name: /关闭恢复|Close restore/ }).click();
  await assertFocusReturned(restoreTrigger);
}

async function assertFocusReturned(trigger) {
  await page.waitForTimeout(50);
  assert.equal(await page.evaluate(() => document.activeElement?.getAttribute("aria-label")), await trigger.getAttribute("aria-label"));
}
