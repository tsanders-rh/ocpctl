# OCPCTL Presentations

This directory contains presentation materials for OCPCTL.

## Available Presentations

### 1. `ocpctl-overview.md` - Full Engineering Team Presentation
**Duration:** 45-60 minutes
**Audience:** Engineering teams, technical stakeholders
**Content:** Complete platform overview with architecture, features, use cases, and demos

**Slides:** 40+ slides covering:
- Platform overview and problem statement
- Supported platforms and features
- Architecture deep dive
- Cost management and ROI
- Security and compliance
- Use cases and success stories
- Getting started guide
- Technical appendices

## Viewing the Presentations

### Option 1: Marp (Recommended)

**Marp** converts Markdown to beautiful slide decks.

1. **Install Marp CLI:**
   ```bash
   npm install -g @marp-team/marp-cli
   ```

2. **Preview in browser:**
   ```bash
   marp ocpctl-overview.md --preview
   ```

3. **Export to PDF:**
   ```bash
   marp ocpctl-overview.md --pdf --allow-local-files
   ```

4. **Export to PowerPoint:**
   ```bash
   marp ocpctl-overview.md --pptx
   ```

5. **Export to HTML:**
   ```bash
   marp ocpctl-overview.md --html
   ```

**Marp VS Code Extension:** Install "Marp for VS Code" for live preview while editing.

### Option 2: reveal.js

**reveal.js** creates interactive HTML presentations.

1. **Clone reveal.js:**
   ```bash
   git clone https://github.com/hakimel/reveal.js.git
   cd reveal.js
   npm install
   ```

2. **Convert Markdown to reveal.js:**
   - Copy slide content to `index.html` in reveal.js template
   - Or use pandoc: `pandoc ocpctl-overview.md -o slides.html -t revealjs -s`

3. **Serve:**
   ```bash
   npm start
   ```

### Option 3: Copy to PowerPoint/Google Slides

1. **Open the Markdown file** in any text editor
2. **Copy slide content** (each `---` separator is a new slide)
3. **Paste into PowerPoint/Google Slides**
4. **Apply your corporate template**

Each slide is clearly separated by `---` markers.

## Customizing Presentations

### Change Theme (Marp)

Edit the frontmatter at the top of the Markdown file:

```yaml
---
marp: true
theme: default  # Options: default, gaia, uncover
paginate: true
---
```

### Add Your Branding

1. Replace URLs with your deployment
2. Update email addresses and Slack channels
3. Add your company logo to slides
4. Adjust color schemes in frontmatter

### Update Metrics

Edit the "Success Metrics" slide with your real data:
- Number of clusters provisioned
- Active users
- Cost savings
- Time saved

## Presentation Tips

### For 60-minute Session:
- Spend 5 min on intro slides (1-5)
- 10 min on features (6-12)
- 10 min on architecture (13-17)
- 5 min on use cases (24-26)
- 5 min on getting started (27-29)
- 10 min on live demo (40)
- 15 min on Q&A

### For 30-minute Session:
- Skip appendix slides (42+)
- Condense architecture to 5 min
- Focus on features and use cases
- 10 min for demo
- 10 min for Q&A

### For 15-minute Session (Executive Brief):
- Slides: 1-6, 10-12, 22-23, 27, 41
- Problem → Solution → ROI → Next Steps
- Skip technical deep dives

## Live Demo Preparation

### Before Presentation:

1. **Test access:** Verify you can login to https://ocpctl.mg.dog8code.com
2. **Pre-create cluster:** Start a cluster 30-40 min before presentation (so it's READY during demo)
3. **Prepare screenshots:** In case of network issues
4. **Test API:** Verify curl commands work with your API key
5. **Download kubeconfig:** Have it ready to show `oc` commands

### Demo Script:

```bash
# 1. Show web UI
open https://ocpctl.mg.dog8code.com

# 2. Navigate to cluster inventory
# - Show different statuses
# - Show cost dashboard
# - Show orphaned resources

# 3. Show cluster details page
# - Click on a READY cluster
# - Show console URL (click to open)
# - Download kubeconfig
# - Show cluster info

# 4. Create new cluster (or show pre-created)
# - Click "Create Cluster"
# - Select profile: aws-sno-ga
# - Show configuration options
# - Click create (or show existing cluster status)

# 5. Show API integration
export API_KEY="ocpctl_YOUR_KEY"

# List clusters
curl https://ocpctl.mg.dog8code.com/api/v1/clusters \
  -H "Authorization: Bearer $API_KEY" | jq

# Get specific cluster
curl https://ocpctl.mg.dog8code.com/api/v1/clusters/$CLUSTER_ID \
  -H "Authorization: Bearer $API_KEY" | jq

# 6. Show kubeconfig usage
export KUBECONFIG=~/Downloads/kubeconfig-demo-cluster.yaml
oc get nodes
oc get clusteroperators
oc whoami --show-console

# 7. Show Jenkins integration
# - Navigate to examples/jenkins/
# - Show Jenkinsfile highlights
# - (Optional) Show running Jenkins job if available
```

### Backup Plan (No Live Demo):

Use screenshots in `docs/screenshots/` directory or record a demo video beforehand.

## Exporting for Different Audiences

### For Executives (PDF):
```bash
# Create PDF with larger fonts, less technical detail
marp ocpctl-overview.md --pdf --theme gaia -o ocpctl-executive-brief.pdf
```

### For Developers (HTML):
```bash
# Create interactive HTML with code syntax highlighting
marp ocpctl-overview.md --html -o ocpctl-technical-overview.html
```

### For Print Handouts:
```bash
# Create PDF with notes
marp ocpctl-overview.md --pdf --allow-local-files --notes -o ocpctl-handout.pdf
```

## Slide Deck Maintenance

### Monthly Updates:
- [ ] Update cluster count in success metrics
- [ ] Update cost savings numbers
- [ ] Update roadmap progress (move items from 🚧 to ✅)
- [ ] Add new profiles to supported platforms slide
- [ ] Update screenshots if UI changes

### Before Each Presentation:
- [ ] Verify all URLs work
- [ ] Test live demo script
- [ ] Update "Today's date" in intro slide
- [ ] Check for outdated metrics
- [ ] Customize for specific audience

## Additional Resources

- **Marp Documentation:** https://marp.app/
- **reveal.js Documentation:** https://revealjs.com/
- **Presentation Best Practices:** https://www.presentation-guru.com/

## Contributing

To improve these presentations:

1. Edit the Markdown files directly
2. Test with Marp preview
3. Update this README if adding new presentations
4. Commit changes with descriptive message

## Questions?

For questions about the presentation content or technical details about OCPCTL:
- Slack: #ocpctl-support
- Email: ocpctl-team@example.com
- GitHub Issues: https://github.com/tsanders-rh/ocpctl/issues
