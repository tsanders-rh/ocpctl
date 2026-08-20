#!/bin/bash
set -e

echo "========================================="
echo "OCPCTL Automated Demo Recorder"
echo "========================================="
echo ""

# Check if in the right directory
if [ ! -f "record-demo.js" ]; then
    echo "Error: Must run from the demo/ directory"
    echo "Run: cd demo && ./run-demo.sh"
    exit 1
fi

# Check if node_modules exists
if [ ! -d "node_modules" ]; then
    echo "📦 Installing dependencies..."
    npm install
    echo ""
fi

# Check if Chromium is installed
if [ ! -d "$HOME/Library/Caches/ms-playwright/chromium-1228" ]; then
    echo "🌐 Installing Playwright Chromium browser..."
    npx playwright install chromium
    echo ""
fi

# Run preflight check
echo "🔍 Running preflight check..."
../scripts/demo-preflight-check.sh
echo ""

# Confirm before starting
echo "========================================="
echo "Ready to record demo!"
echo "========================================="
echo ""
echo "This will:"
echo "  • Open a browser window (you'll see it in action)"
echo "  • Navigate through the ocpctl UI automatically"
echo "  • Record video to demo/recordings/"
echo "  • Take screenshots to demo/screenshots/"
echo "  • Take approximately 10-12 minutes"
echo ""
read -p "Press Enter to start recording, or Ctrl+C to cancel..."
echo ""

# Start recording
echo "🎬 Starting demo recording..."
echo ""

npm run record

echo ""
echo "========================================="
echo "✅ Demo recording complete!"
echo "========================================="
echo ""
echo "Your recordings are in:"
echo "  • Video: demo/recordings/"
echo "  • Screenshots: demo/screenshots/"
echo ""

# Show the video file
VIDEO_FILE=$(ls -t recordings/*.webm 2>/dev/null | head -1)
if [ -n "$VIDEO_FILE" ]; then
    VIDEO_SIZE=$(du -h "$VIDEO_FILE" | cut -f1)
    echo "📹 Video file: $VIDEO_FILE ($VIDEO_SIZE)"
    echo ""
    echo "To convert to MP4:"
    echo "  ffmpeg -i \"$VIDEO_FILE\" -c:v libx264 -crf 18 -c:a aac demo.mp4"
    echo ""
    echo "To play the video:"
    echo "  open \"$VIDEO_FILE\""
fi

echo ""
echo "Next steps:"
echo "  1. Review the video and screenshots"
echo "  2. Convert WebM to MP4 using ffmpeg (see above)"
echo "  3. Add narration if desired"
echo "  4. Share with your team!"
