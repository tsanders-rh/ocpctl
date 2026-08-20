import { chromium } from 'playwright';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import { mkdirSync, existsSync } from 'fs';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// Configuration
const CONFIG = {
  url: 'https://dev.ocpctl.mg.dog8code.com',
  email: 'admin@example.com',
  password: 'changeme',
  videoDir: join(__dirname, 'recordings'),
  screenshotsDir: join(__dirname, 'screenshots'),
  clusterName: 'demo-platform-overview',
  slowMo: 500, // Milliseconds to slow down operations (makes it more watchable)
};

// Ensure output directories exist
[CONFIG.videoDir, CONFIG.screenshotsDir].forEach(dir => {
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }
});

// Helper function to wait and make actions more visible
async function pause(ms = 1000) {
  await new Promise(resolve => setTimeout(resolve, ms));
}

// Helper to type slowly (more natural)
async function typeSlowly(page, selector, text, delay = 100) {
  await page.click(selector);
  await page.type(selector, text, { delay });
}

async function recordDemo() {
  console.log('🎬 Starting OCPCTL Demo Recording...\n');

  // Launch browser with video recording
  const browser = await chromium.launch({
    headless: false, // Show the browser so you can see what's happening
    slowMo: CONFIG.slowMo,
    args: [
      '--start-maximized',
      '--disable-blink-features=AutomationControlled',
    ],
  });

  const context = await browser.newContext({
    viewport: { width: 1920, height: 1080 },
    recordVideo: {
      dir: CONFIG.videoDir,
      size: { width: 1920, height: 1080 },
    },
    ignoreHTTPSErrors: true, // In case of self-signed certs
  });

  const page = await context.newPage();
  let screenshotCounter = 1;

  const screenshot = async (name) => {
    const filename = `${String(screenshotCounter).padStart(2, '0')}-${name}.png`;
    await page.screenshot({
      path: join(CONFIG.screenshotsDir, filename),
      fullPage: false
    });
    console.log(`📸 Screenshot: ${filename}`);
    screenshotCounter++;
  };

  try {
    // ===================================================================
    // Act 1: Introduction & Login (0:00 - 1:30)
    // ===================================================================
    console.log('Act 1: Introduction & Login');

    await page.goto(CONFIG.url);
    await pause(2000);
    await screenshot('01-landing-page');

    // Look for login form - adjust selectors based on your actual UI
    console.log('  → Logging in...');

    // Common login selectors - adjust based on your actual form
    try {
      // Try to find email/username field (common selectors)
      const emailSelector = 'input[type="email"], input[name="email"], input[placeholder*="email" i], input[name="username"]';
      await page.waitForSelector(emailSelector, { timeout: 5000 });
      await typeSlowly(page, emailSelector, CONFIG.email);
      await pause(500);

      // Password field
      const passwordSelector = 'input[type="password"], input[name="password"]';
      await typeSlowly(page, passwordSelector, CONFIG.password);
      await pause(500);

      await screenshot('02-login-form-filled');

      // Submit button (try multiple common patterns)
      const submitSelector = 'button[type="submit"], button:has-text("Login"), button:has-text("Sign in"), input[type="submit"]';
      await page.click(submitSelector);
      await pause(3000);

    } catch (error) {
      console.log('  ⚠️  Could not find login form - might already be logged in or different structure');
      console.log('  ℹ️  Error:', error.message);
    }

    // ===================================================================
    // Act 2: Dashboard Overview (1:30 - 2:30)
    // ===================================================================
    console.log('\nAct 2: Dashboard Overview');

    // Wait for dashboard to load
    await page.waitForLoadState('networkidle');
    await pause(2000);
    await screenshot('03-dashboard');

    // Scroll down to show any existing clusters or stats
    await page.evaluate(() => window.scrollBy(0, 300));
    await pause(1000);
    await page.evaluate(() => window.scrollTo(0, 0));
    await pause(1000);

    // ===================================================================
    // Act 3: Cluster Creation (2:30 - 5:00)
    // ===================================================================
    console.log('\nAct 3: Cluster Creation');

    // Click "Create Cluster" button (adjust selector based on your UI)
    console.log('  → Navigating to cluster creation...');
    try {
      const createButtonSelector = 'button:has-text("Create Cluster"), a:has-text("Create Cluster"), [data-testid="create-cluster"]';
      await page.click(createButtonSelector, { timeout: 5000 });
      await pause(2000);
    } catch (error) {
      console.log('  ℹ️  Trying navigation to /clusters/create directly...');
      await page.goto(`${CONFIG.url}/clusters/create`);
      await pause(2000);
    }

    await screenshot('04-create-cluster-form');

    // Fill in cluster creation form
    console.log('  → Filling cluster form...');

    // Cluster name
    console.log('    • Cluster name');
    const nameSelector = 'input[name="name"], input[placeholder*="name" i], #cluster-name, #name';
    await page.waitForSelector(nameSelector, { timeout: 10000 });
    await typeSlowly(page, nameSelector, CONFIG.clusterName, 80);
    await pause(1000);

    // Profile selection
    console.log('    • Selecting profile');
    try {
      // Look for profile dropdown/select
      const profileSelector = 'select[name="profile"], select[name="profileName"], #profile';
      await page.selectOption(profileSelector, { label: /standard/i });
      await pause(1500);
    } catch (error) {
      console.log('    ⚠️  Could not select profile automatically - may need manual adjustment');
    }

    // Version selection
    console.log('    • Selecting version');
    try {
      const versionSelector = 'select[name="version"], #version';
      await page.selectOption(versionSelector, { index: 1 }); // Select first available version
      await pause(1500);
    } catch (error) {
      console.log('    ⚠️  Could not select version - may be auto-selected');
    }

    // Region selection
    console.log('    • Selecting region');
    try {
      const regionSelector = 'select[name="region"], #region';
      await page.selectOption(regionSelector, 'us-east-1');
      await pause(1500);
    } catch (error) {
      console.log('    ⚠️  Could not select region - may be auto-selected');
    }

    await screenshot('05-basic-info-filled');

    // Scroll to show more form fields
    await page.evaluate(() => window.scrollBy(0, 400));
    await pause(1500);

    // Tags section
    console.log('    • Adding tags');
    try {
      // This is highly dependent on your UI - adjust as needed
      const tagKeySelector = 'input[placeholder*="key" i], input[name*="tag"][name*="key"]';
      const tagValueSelector = 'input[placeholder*="value" i], input[name*="tag"][name*="value"]';

      await page.fill(tagKeySelector, 'team');
      await pause(300);
      await page.fill(tagValueSelector, 'platform-engineering');
      await pause(800);

      // Try to add another tag
      const addTagButton = 'button:has-text("Add Tag"), button:has-text("+")';
      await page.click(addTagButton);
      await pause(500);
    } catch (error) {
      console.log('    ⚠️  Could not add tags - UI may differ');
    }

    await screenshot('06-tags-added');

    // Scroll to addons section
    console.log('    • Selecting addons');
    await page.evaluate(() => window.scrollBy(0, 400));
    await pause(2000);

    // Enable CNV addon
    try {
      const cnvCheckbox = 'input[type="checkbox"][value*="cnv" i], input[name*="cnv"], label:has-text("Virtualization") input';
      await page.check(cnvCheckbox);
      await pause(1500);

      await screenshot('07-addon-cnv-selected');

      // Try to select CNV version
      const cnvVersionSelector = 'select[name*="cnv"], select:near(label:has-text("Virtualization"))';
      await page.selectOption(cnvVersionSelector, { label: /4.22/i });
      await pause(1000);

      // Enable Windows VM option
      const windowsCheckbox = 'input[type="checkbox"][name*="windows"], label:has-text("Windows") input';
      await page.check(windowsCheckbox);
      await pause(1500);

      await screenshot('08-addon-windows-selected');
    } catch (error) {
      console.log('    ⚠️  Could not configure addons - UI may differ');
    }

    // Scroll to lifecycle section
    await page.evaluate(() => window.scrollBy(0, 400));
    await pause(2000);
    await screenshot('09-lifecycle-settings');

    // Scroll to bottom to find submit button
    await page.evaluate(() => window.scrollBy(0, 400));
    await pause(1500);

    // Submit the form
    console.log('  → Submitting cluster creation...');
    await screenshot('10-ready-to-submit');

    try {
      const submitButton = 'button[type="submit"]:has-text("Create"), button:has-text("Create Cluster")';
      await page.click(submitButton);
      await pause(3000);
      console.log('  ✅ Cluster creation submitted!');
    } catch (error) {
      console.log('  ⚠️  Could not find submit button - you may need to click manually');
    }

    // Should now be on cluster details page
    await page.waitForLoadState('networkidle');
    await screenshot('11-cluster-creating');

    // Show cluster status
    await pause(3000);

    // ===================================================================
    // Act 4: Platform Management Features (5:00 - 6:30)
    // ===================================================================
    console.log('\nAct 4: Platform Management Features');

    // Navigate to Profiles
    console.log('  → Viewing profiles...');
    try {
      await page.click('a:has-text("Profiles"), [href*="profiles"]');
      await pause(2000);
      await screenshot('12-profiles-list');

      // Scroll through profiles
      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);
      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);

      // Click on a profile to show details
      const profileLink = 'a:has-text("standard"), tr:has-text("standard") a, [data-testid*="profile"]';
      await page.click(profileLink);
      await pause(2000);
      await screenshot('13-profile-details');

      // Scroll through profile details
      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);
      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);
    } catch (error) {
      console.log('  ⚠️  Could not navigate to profiles');
    }

    // Navigate to Addons
    console.log('  → Viewing addons...');
    try {
      await page.click('a:has-text("Addons"), [href*="addons"]');
      await pause(2000);
      await screenshot('14-addons-list');

      // Click on CNV addon
      const cnvAddon = 'a:has-text("Virtualization"), a:has-text("CNV"), tr:has-text("CNV") a';
      await page.click(cnvAddon);
      await pause(2000);
      await screenshot('15-addon-details');

      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);
    } catch (error) {
      console.log('  ⚠️  Could not navigate to addons');
    }

    // ===================================================================
    // Act 5: Cost Management (6:30 - 7:30)
    // ===================================================================
    console.log('\nAct 5: Cost Management');

    // Navigate back to clusters
    console.log('  → Viewing cluster cost info...');
    try {
      await page.click('a:has-text("Clusters"), [href*="clusters"]');
      await pause(2000);
      await screenshot('16-clusters-list-with-costs');

      // Click on our demo cluster
      await page.click(`a:has-text("${CONFIG.clusterName}"), tr:has-text("${CONFIG.clusterName}") a`);
      await pause(2000);
      await screenshot('17-cluster-details-costs');

      // Scroll to show cost information
      await page.evaluate(() => window.scrollBy(0, 300));
      await pause(1500);
    } catch (error) {
      console.log('  ⚠️  Could not navigate to cluster details');
    }

    // Try to show orphaned resources if available
    try {
      await page.click('a:has-text("Orphaned"), [href*="orphaned"]');
      await pause(2000);
      await screenshot('18-orphaned-resources');
    } catch (error) {
      console.log('  ℹ️  Orphaned resources page not accessible');
    }

    // ===================================================================
    // Act 6: Cluster Lifecycle Operations (7:30 - 8:30)
    // ===================================================================
    console.log('\nAct 6: Cluster Lifecycle Operations');

    // Navigate back to our cluster
    console.log('  → Showing lifecycle controls...');
    try {
      await page.click('a:has-text("Clusters")');
      await pause(1000);
      await page.click(`a:has-text("${CONFIG.clusterName}")`);
      await pause(2000);

      // Scroll to show action buttons
      await page.evaluate(() => window.scrollTo(0, 200));
      await pause(1500);
      await screenshot('19-lifecycle-actions');

      // Hover over buttons to highlight them (don't click!)
      try {
        await page.hover('button:has-text("Hibernate")');
        await pause(1000);
        await page.hover('button:has-text("Destroy")');
        await pause(1000);
        await page.hover('button:has-text("Download"), a:has-text("Kubeconfig")');
        await pause(1000);
      } catch (error) {
        console.log('  ℹ️  Could not hover over action buttons');
      }

      // Scroll to outputs section
      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);
      await screenshot('20-cluster-outputs');

      // Scroll to events/audit log
      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);
      await screenshot('21-cluster-events');
    } catch (error) {
      console.log('  ⚠️  Could not show lifecycle operations');
    }

    // ===================================================================
    // Act 7: Multi-Cloud Capabilities (8:30 - 9:30)
    // ===================================================================
    console.log('\nAct 7: Multi-Cloud Capabilities');

    console.log('  → Showing multi-cloud profiles...');
    try {
      await page.click('a:has-text("Profiles")');
      await pause(2000);

      // Filter or search for different cloud platforms
      // Scroll to show variety
      await page.evaluate(() => window.scrollTo(0, 0));
      await pause(1000);
      await screenshot('22-multicloud-profiles');

      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);
      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(1500);
      await screenshot('23-multicloud-profiles-list');
    } catch (error) {
      console.log('  ⚠️  Could not show multi-cloud profiles');
    }

    // ===================================================================
    // Act 8: Closing (9:30 - 10:00)
    // ===================================================================
    console.log('\nAct 8: Closing');

    // Navigate back to dashboard for final view
    console.log('  → Final dashboard view...');
    try {
      await page.click('a:has-text("Dashboard"), a:has-text("Home"), [href="/"]');
      await pause(2000);
      await screenshot('24-final-dashboard');

      // Scroll to show the created cluster
      await page.evaluate(() => window.scrollBy(0, 400));
      await pause(2000);
      await page.evaluate(() => window.scrollTo(0, 0));
      await pause(3000);

      await screenshot('25-demo-complete');
    } catch (error) {
      console.log('  ⚠️  Could not return to dashboard');
    }

    console.log('\n✅ Demo recording complete!');

    // Keep browser open for a moment before closing
    await pause(2000);

  } catch (error) {
    console.error('\n❌ Error during demo recording:', error);
    await screenshot('error-state');
  } finally {
    // Close browser and save video
    console.log('\n💾 Saving video...');
    await context.close();
    await browser.close();

    console.log('\n📦 Demo artifacts:');
    console.log(`   Video: ${CONFIG.videoDir}/`);
    console.log(`   Screenshots: ${CONFIG.screenshotsDir}/`);
    console.log('\n🎉 All done! Check the recordings directory for your video.');
  }
}

// Run the demo
recordDemo().catch(console.error);
