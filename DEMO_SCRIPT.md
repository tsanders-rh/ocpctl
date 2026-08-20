# OCPCTL Demo Script - Platform Overview

**Duration**: 8-10 minutes
**Environment**: Dev (https://dev.ocpctl.mg.dog8code.com)
**Focus**: End-to-end cluster lifecycle management
**Audience**: Platform teams, developers, stakeholders

---

## Pre-Demo Checklist

### Environment Preparation
- [ ] Dev environment is healthy: `ssh -i ~/.ssh/ocpctl-dev-key ubuntu@54.167.79.11 'sudo systemctl status ocpctl-api ocpctl-worker ocpctl-web'`
- [ ] No existing demo clusters (clean slate)
- [ ] Browser cache cleared, logged out
- [ ] Browser zoom at 100% or 110% for readability
- [ ] Close unnecessary browser tabs
- [ ] Hide bookmarks bar for clean recording
- [ ] Notifications disabled (Do Not Disturb mode)

### Recording Setup
- [ ] Screen resolution: 1920x1080 (16:9) or 1280x720
- [ ] Recording tool configured (see recommendations below)
- [ ] Audio input tested (clear narration)
- [ ] Demo script printed or on second monitor

### Content Preparation
- [ ] Profile to use: `aws-openshift-standard-ga` (3-node cluster)
- [ ] Addons to enable: CNV 4.22 with Windows VM
- [ ] Region: `us-east-1` (has Windows EBS snapshots for fast deployment)
- [ ] Practice run completed (timing verified)

---

## Demo Flow & Script

### Act 1: Introduction (0:00 - 1:30)

**[Screen: Landing page or login]**

**Narration**:
> "Hi everyone, I'm excited to show you ocpctl - our internal platform for managing OpenShift and Kubernetes clusters across AWS, GCP, and IBM Cloud. This demo will walk through the complete lifecycle of a cluster: from creation to management to teardown. Let's get started."

**Actions**:
1. Navigate to `https://dev.ocpctl.mg.dog8code.com`
2. Login with `admin@example.com` / `changeme`

---

### Act 2: Platform Overview (1:30 - 2:30)

**[Screen: Dashboard]**

**Narration**:
> "Here's our dashboard. Right now it's empty, but this is where you'd see all your active clusters, their status, and key metrics. ocpctl supports OpenShift on AWS, GCP, and IBM Cloud, as well as managed Kubernetes like EKS, GKE, and IKS. We have over 30 pre-configured profiles that handle everything from single-node development clusters to multi-node production environments."

**Actions**:
1. Briefly show empty dashboard
2. Click "Create Cluster" button

---

### Act 3: Cluster Creation (2:30 - 5:00)

**[Screen: Create cluster form]**

**Narration**:
> "Let's create a 3-node OpenShift cluster. The form makes this really simple. I'll select the 'standard' profile which gives us a production-ready configuration with smart defaults."

**Actions**:
1. Fill in form:
   - **Cluster Name**: `demo-cluster` (or `demo-prod-1`)
   - **Profile**: Select `aws-openshift-standard-ga`
   - **Version**: Select `4.20` (or latest available)
   - **Region**: `us-east-1`
   - **Tags**: Add `team: platform-engineering`, `purpose: demo`

**Narration**:
> "One of the powerful features is our addon system. I'm going to enable OpenShift Virtualization, which includes support for running Windows VMs. This is great for teams that need mixed workloads."

**Actions**:
2. Scroll to "Post-Deployment Addons" section
3. Check "OpenShift Virtualization (CNV)"
4. Select version `4.22 stable-stage`
5. Enable "Include Windows 10 VM"

**Narration**:
> "Notice the lifecycle settings - we enforce TTLs to prevent forgotten clusters from racking up costs, and we automatically hibernate clusters outside work hours. This has saved us thousands of dollars per month."

**Actions**:
6. Scroll to "Lifecycle" section (show TTL: 72 hours, Auto-hibernate enabled)
7. Click "Create Cluster"

**[Screen: Cluster details page, status: CREATING]**

**Narration**:
> "And we're off! The cluster is now being provisioned. This typically takes 30-40 minutes for a full OpenShift cluster. The platform handles everything: VPC creation, load balancers, control plane, worker nodes, and then our post-deployment addons. All of this runs asynchronously through our worker service, which you can scale independently from the API."

**Actions**:
8. Show cluster status: CREATING
9. Scroll down to show "Events" or "Job Progress" (if visible)
10. Show estimated completion time

---

### Act 4: Platform Management Features (5:00 - 6:30)

**[Screen: Navigate to Profiles or Clusters list]**

**Narration**:
> "While that's creating, let me show you some management features. We have a profile system that standardizes cluster configurations. Each profile defines allowed versions, regions, instance types, and lifecycle policies. This ensures consistency and prevents configuration drift."

**Actions**:
1. Click "Profiles" in navigation
2. Show list of profiles (scroll through to show variety)
3. Click on `aws-openshift-standard-ga` to show details
4. Highlight key fields: versions, regions, compute settings, TTL

**Narration**:
> "The addon system is equally flexible. We currently support OpenShift Virtualization, Migration Toolkit for Applications, Migration Toolkit for Containers, and OADP for backup and restore. These are all versioned and deployed automatically after cluster creation."

**Actions**:
5. Click "Addons" in navigation
6. Show list of available addons
7. Click on CNV addon to show supported versions and capabilities

---

### Act 5: Cost Management (6:30 - 7:30)

**[Screen: Back to cluster details or dashboard]**

**Narration**:
> "Cost management is built right in. Every cluster shows its hourly cost based on instance types and services. When we hibernate a cluster, we stop all EC2 instances, which reduces the cost by about 90% - you only pay for storage."

**Actions**:
1. Navigate back to `demo-cluster` details
2. Show cost information (if displayed)
3. Explain hibernation toggle (show but don't click since it's still creating)

**Narration**:
> "We also have automated cleanup. Clusters that hit their TTL are automatically destroyed. We track orphaned AWS resources and make them easy to clean up. This prevents the classic problem of forgotten infrastructure running up bills."

**Actions**:
4. Navigate to "Orphaned Resources" or "Cost Tracking" (if available in UI)
5. Show any example data or explain the feature

---

### Act 6: Cluster Lifecycle Operations (7:30 - 8:30)

**[Screen: Cluster details]**

**Narration**:
> "Once your cluster is ready, you have full lifecycle control. You can download the kubeconfig directly from the UI, get the console URL and credentials, hibernate the cluster when not in use, resume it when needed, and destroy it when you're done."

**Actions**:
1. Show action buttons: "Hibernate", "Resume", "Destroy", "Download Kubeconfig"
2. Scroll to "Outputs" section (show API URL, console URL placeholders)

**Narration**:
> "All actions are audit logged, so you have complete traceability of who did what and when. This is critical for compliance and troubleshooting."

**Actions**:
3. Scroll to "Events" or "Audit Log" section (if visible)
4. Show example events

---

### Act 7: Multi-Cloud & Architecture (8:30 - 9:30)

**[Screen: Create cluster or profiles page]**

**Narration**:
> "I mentioned multi-cloud earlier. Let me show you how easy it is to deploy to other platforms."

**Actions**:
1. Navigate to "Create Cluster" or "Profiles"
2. Filter or show profiles for EKS, GKE, IKS
3. Show that the form is nearly identical across platforms

**Narration**:
> "Behind the scenes, ocpctl uses a modular architecture. We have platform-specific handlers for OpenShift's installer, eksctl for EKS, gcloud for GKE, and ibmcloud CLI for IKS. The API and worker service are written in Go for performance, and this web UI is built with Next.js. Everything is backed by PostgreSQL for job queuing and state management."

**Actions**:
4. Optionally show a quick architecture diagram (if you have one) or just narrate

---

### Act 8: Closing & Call to Action (9:30 - 10:00)

**[Screen: Dashboard or cluster list]**

**Narration**:
> "To wrap up: ocpctl makes cluster lifecycle management simple, consistent, and cost-effective. You can provision production-ready OpenShift or Kubernetes clusters across multiple clouds in just a few clicks, with automated lifecycle management and built-in cost controls. We've saved significant time and money by standardizing on this platform."
>
> "If you're interested in using ocpctl or have questions, reach out to the platform engineering team. The platform is ready for production use, and we're here to help you get started. Thanks for watching!"

**Actions**:
1. Navigate to dashboard showing the demo-cluster (still creating)
2. Show final overview
3. End recording

---

## Post-Recording Cleanup

### After Demo Recording
```bash
# SSH to dev
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@54.167.79.11

# Check if demo cluster is still creating
# If you want to keep it running for screenshots or follow-up demos, leave it
# If you want to clean up:
# 1. Navigate to cluster in UI
# 2. Click "Destroy" button
# 3. Confirm destruction
```

### Editing Checklist
- [ ] Trim dead air at start/end
- [ ] Add title slide (optional): "OCPCTL - Cluster Lifecycle Management Platform"
- [ ] Add lower-third with your name/title (optional)
- [ ] Add chapter markers at each Act (if platform supports)
- [ ] Normalize audio levels
- [ ] Export at 1080p or 720p (H.264, MP4)
- [ ] Test playback on different devices

---

## Recording Tools Recommendations

### macOS
- **QuickTime Player** (Built-in, Free)
  - Simple screen recording with system audio
  - `File → New Screen Recording`
  - Good for quick demos, limited editing

- **ScreenFlow** ($169, Recommended)
  - Professional screen recording + editing
  - Built-in annotations, transitions, titles
  - Multi-track editing for audio/video

- **OBS Studio** (Free, Open Source)
  - Powerful, highly customizable
  - Steep learning curve but very capable
  - Good for streaming and recording

### Windows
- **OBS Studio** (Free, Open Source)
  - Industry standard for screen recording
  - Supports multiple sources, scenes

- **Camtasia** ($249)
  - Professional recording + editing suite
  - Easy to use, great for tutorials

### Linux
- **OBS Studio** (Free, Open Source)
- **SimpleScreenRecorder** (Free)
  - Lightweight, easy to use
  - Good for basic screen capture

### Recommended Settings
- **Resolution**: 1920x1080 or 1280x720
- **Frame Rate**: 30 fps (60 fps if showing animations)
- **Bitrate**: 5-10 Mbps (for 1080p)
- **Format**: MP4 (H.264 video, AAC audio)
- **Audio**: 192 kbps stereo or 128 kbps mono

---

## Tips for Great Demo Recording

### Preparation
1. Practice 2-3 times before recording
2. Have a glass of water nearby
3. Close all unnecessary applications
4. Use Do Not Disturb mode
5. Disable desktop notifications
6. Hide desktop icons (optional, for clean look)

### Narration
1. Speak clearly and at moderate pace (not too fast)
2. Pause briefly between sections
3. Use active voice: "I'm creating a cluster" not "The cluster is being created"
4. Avoid filler words: "um", "uh", "like", "you know"
5. Smile while talking (it comes through in your voice)

### Screen Work
1. Move mouse smoothly and deliberately
2. Highlight important fields by hovering cursor
3. Don't rush through forms - let viewers read
4. Zoom in if text is small (Cmd+Plus on Mac, Ctrl+Plus on Windows)
5. Use full screen browser mode (F11) for cleaner look

### Pacing
1. Allow 2-3 seconds of pause between major actions
2. Don't rush - better to be 10 minutes than feel rushed at 8
3. If you make a mistake, pause for 5 seconds, then start the sentence again (easy to edit out)

### Common Mistakes to Avoid
- Typing passwords visibly (use browser autofill)
- Forgetting to enable audio recording
- Recording at odd resolution (stick to 16:9)
- Not checking recording is actually running
- Ignoring audio quality (use decent mic if possible)

---

## Alternative Demo Scenarios

If you want variations of this demo, here are other options:

### Developer-Focused Demo (7 min)
Focus on speed and convenience:
1. Quick cluster creation with SNO profile (single node)
2. Show kubeconfig download and `oc` CLI usage
3. Deploy sample app
4. Hibernate cluster at end of day
5. Resume next morning
6. Show cost savings

### Admin/Platform Team Demo (12 min)
Focus on management and operations:
1. Profile system deep-dive
2. Addon version management
3. Cost tracking and orphaned resource cleanup
4. Worker service job queue (show database or logs)
5. Audit trail and compliance
6. Multi-tenancy and RBAC (if implemented)

### Architecture Deep-Dive (20 min)
For technical audience:
1. System architecture diagram
2. Database schema and migrations
3. Job queue and distributed locking
4. Worker service scaling
5. S3 artifact management
6. Deployment process and versioning
7. SSH to server and show logs, systemd services

---

## Questions or Issues?

If you run into any problems during demo recording:
- Dev environment down: Check systemd services
- Cluster creation failing: Check worker logs
- UI not loading: Check web service and nginx
- Need different cluster type: Use EKS or GKE profile instead

Good luck with your demo! This platform is impressive and the demo will showcase that well.
