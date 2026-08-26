# OCPCTL Automated Demo Recorder

This directory contains a Playwright-based automated demo recorder that will navigate through the ocpctl web UI and record a video demonstration.

## Quick Start

```bash
# 1. Install dependencies
cd demo
npm install

# 2. Install Playwright browsers
npm run install-browsers

# 3. Run the demo recorder
npm run record
```

The script will:
- Open a browser window (visible, not headless)
- Navigate through the demo flow automatically
- Record video of the entire session
- Take screenshots at key moments
- Save everything to `recordings/` and `screenshots/`

## What Gets Recorded

The automated demo follows this flow (8-10 minutes):

1. **Login** - Authenticates to dev environment
2. **Dashboard** - Shows empty state
3. **Create Cluster** - Fills form for 3-node OpenShift cluster
4. **Addons** - Enables CNV with Windows VM support
5. **Profiles** - Shows available cluster profiles
6. **Addons List** - Displays addon ecosystem
7. **Cost Info** - Demonstrates cost tracking
8. **Lifecycle** - Shows hibernate/resume/destroy controls
9. **Multi-cloud** - Highlights different platform profiles
10. **Final View** - Returns to dashboard with created cluster

## Output

After running, you'll find:

```
demo/
├── recordings/
│   └── [timestamp].webm    # Full video recording (WebM format)
└── screenshots/
    ├── 01-landing-page.png
    ├── 02-login-form-filled.png
    ├── 03-dashboard.png
    ├── 04-create-cluster-form.png
    └── ... (25+ screenshots)
```

## Converting Video to MP4

Playwright records in WebM format. To convert to MP4:

```bash
# Install ffmpeg if you don't have it
brew install ffmpeg

# Convert to MP4
ffmpeg -i recordings/[your-video].webm -c:v libx264 -c:a aac -strict experimental demo.mp4

# Or with better quality
ffmpeg -i recordings/[your-video].webm -c:v libx264 -preset slow -crf 18 -c:a aac -b:a 192k demo.mp4
```

## Customization

### Change Demo Settings

Edit `record-demo.js` and modify the `CONFIG` object:

```javascript
const CONFIG = {
  url: 'https://dev.ocpctl.<BASE_DOMAIN>',  // Change environment
  email: 'admin@example.com',                  // Change credentials
  password: 'changeme',
  clusterName: 'demo-platform-overview',        // Change cluster name
  slowMo: 500,                                  // Adjust speed (ms delay between actions)
};
```

### Adjust Timing

The script uses `pause()` functions throughout. To make the demo:
- **Faster**: Reduce `CONFIG.slowMo` and `pause()` durations
- **Slower**: Increase `CONFIG.slowMo` and `pause()` durations

### Change Resolution

Edit the `viewport` and `recordVideo` settings in `record-demo.js`:

```javascript
const context = await browser.newContext({
  viewport: { width: 1920, height: 1080 },  // Change resolution
  recordVideo: {
    dir: CONFIG.videoDir,
    size: { width: 1920, height: 1080 },    // Must match viewport
  },
});
```

Common resolutions:
- 1920x1080 (Full HD)
- 1280x720 (HD)
- 3840x2160 (4K - may cause performance issues)

## Troubleshooting

### "Cannot find element" errors

The script uses common selectors, but your UI might differ. If the script fails:

1. **Run in non-headless mode** (already the default) to see what's happening
2. **Check the screenshots** in `screenshots/` to see where it failed
3. **Update selectors** in `record-demo.js` to match your actual UI
4. **Use browser DevTools** to inspect elements and get correct selectors

### Selectors to adjust

The most likely selectors that need adjustment:

```javascript
// Login form
const emailSelector = 'input[type="email"]';  // Line ~75
const passwordSelector = 'input[type="password"]';  // Line ~81

// Create cluster button
const createButtonSelector = 'button:has-text("Create Cluster")';  // Line ~112

// Form fields
const nameSelector = 'input[name="name"]';  // Line ~124
const profileSelector = 'select[name="profile"]';  // Line ~131
```

### Video not recording

Make sure you have enough disk space and permissions:

```bash
# Check disk space
df -h .

# Verify directories are writable
ls -la recordings/ screenshots/
```

### Browser doesn't close

If the browser window stays open after completion:
- Check for errors in the console
- Manually close the browser
- The video should still be saved

## Advanced Usage

### Run in Headless Mode

Edit `record-demo.js` line ~56:

```javascript
const browser = await chromium.launch({
  headless: true,  // Change to true for background recording
  // ...
});
```

### Record Multiple Demos

Create different demo scripts for different scenarios:

```bash
# Copy the base script
cp record-demo.js record-demo-developer.js

# Edit to focus on different features
# Then run:
node record-demo-developer.js
```

### Add Narration Later

The automated recording captures the visual flow. You can:

1. Record the video with this script
2. Watch it back to write a narration script
3. Record your voice separately
4. Combine them in video editing software (iMovie, Final Cut, Premiere, etc.)

Or use text-to-speech:

```bash
# macOS has built-in text-to-speech
say -o narration.aiff -f narration.txt

# Convert to MP3
ffmpeg -i narration.aiff narration.mp3

# Combine with video in editing software
```

## Tips for Best Results

1. **Close other apps** - Reduce system load for smoother recording
2. **Run preflight check first** - Ensure dev environment is healthy:
   ```bash
   ../scripts/demo-preflight-check.sh
   ```
3. **Clean up old clusters** - Remove any demo clusters from previous runs
4. **Practice run** - Do a test run first to verify all selectors work
5. **Check the screenshots** - They're great for debugging and can be used standalone
6. **High-quality export** - Use low CRF value (18-20) when converting to MP4

## Next Steps

After recording:

1. **Review the video** - Watch it in a media player
2. **Convert to MP4** - Use ffmpeg (see above)
3. **Edit if needed** - Trim start/end, add titles, combine with narration
4. **Share** - Upload to internal wiki, YouTube, or share directly

## Support

If you encounter issues:

1. Check the console output for error messages
2. Look at the last screenshot to see where it failed
3. Inspect your UI to verify selectors match
4. Adjust the script as needed

The script is designed to be resilient with try/catch blocks, so even if some parts fail, it should continue and capture what it can.
