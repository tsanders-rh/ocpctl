# Updating the Windows QCOW2 Image

This guide walks through updating the Windows 10 QCOW2 image to enable qemu-guest-agent logging (and other modifications).

## Prerequisites

### Required Software

1. **KVM/QEMU** (Linux workstation or VM)
   ```bash
   # Ubuntu/Debian
   sudo apt-get install qemu-kvm libvirt-daemon-system libvirt-clients bridge-utils virt-manager

   # RHEL/Fedora
   sudo dnf install @virtualization
   ```

2. **AWS CLI** (configured with credentials)
   ```bash
   aws configure
   ```

3. **Disk space**: ~50GB free for image manipulation

### Access Required

- S3 read/write access to `ocpctl-binaries` bucket
- Ability to run KVM/QEMU (requires nested virtualization support)

---

## Step-by-Step Process

### Step 1: Download Current Image

```bash
# Create working directory
mkdir -p ~/windows-image-update
cd ~/windows-image-update

# Download current image from S3 (23GB - takes ~5-10 minutes)
aws s3 cp s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2 \
  ./windows-10-oadp-v1.0.qcow2

# Verify download
ls -lh windows-10-oadp-v1.0.qcow2
# Should show: ~23GB file
```

### Step 2: Create Working Copy

```bash
# Create a copy to work with (preserves original)
cp windows-10-oadp-v1.0.qcow2 windows-10-oadp-v1.1.qcow2

# Optional: Create backup
aws s3 cp s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2 \
  s3://ocpctl-binaries/windows-images/backups/windows-10-oadp-v1.0-backup-$(date +%Y%m%d).qcow2
```

### Step 3: Boot Image in KVM

#### Option A: Using virt-manager (GUI)

1. Open virt-manager: `virt-manager`
2. File → New Virtual Machine
3. Select "Import existing disk image"
4. Browse to: `~/windows-image-update/windows-10-oadp-v1.1.qcow2`
5. OS: Windows 10
6. Memory: 4096 MB, CPUs: 2
7. Finish and start VM

#### Option B: Using virt-install (CLI)

```bash
sudo virt-install \
  --name windows10-update \
  --memory 4096 \
  --vcpus 2 \
  --disk path=$HOME/windows-image-update/windows-10-oadp-v1.1.qcow2,format=qcow2 \
  --import \
  --os-variant win10 \
  --graphics vnc,listen=0.0.0.0 \
  --noautoconsole

# Connect to VNC console
virt-viewer windows10-update
```

#### Option C: Using QEMU directly

```bash
qemu-system-x86_64 \
  -enable-kvm \
  -m 4096 \
  -smp 2 \
  -drive file=$HOME/windows-image-update/windows-10-oadp-v1.1.qcow2,format=qcow2 \
  -vnc :1

# Connect via VNC viewer to localhost:5901
```

### Step 4: Configure qemu-guest-agent Logging

Once Windows boots:

1. **Login** to Windows (credentials should be in OADP image documentation)

2. **Open PowerShell as Administrator**
   - Right-click Start → Windows PowerShell (Admin)

3. **Enable qemu-guest-agent verbose logging**
   ```powershell
   # Configure service to run with verbose logging
   sc.exe config QEMU-GA start=auto
   sc.exe config QEMU-GA type=own
   sc.exe config QEMU-GA binpath="C:\Program Files\Qemu-ga\qemu-ga.exe -d -v"

   # Restart service to apply changes
   sc.exe stop QEMU-GA
   Start-Sleep -Seconds 5
   sc.exe start QEMU-GA
   ```

4. **Verify logging is enabled**
   ```powershell
   # Check service configuration
   sc.exe query QEMU-GA
   sc.exe qc QEMU-GA

   # Should show:
   # START_TYPE: AUTO_START
   # BINARY_PATH_NAME: C:\Program Files\Qemu-ga\qemu-ga.exe -d -v

   # Verify log file exists
   $PID = (Get-WmiObject Win32_Service | Where-Object { $_.Name -eq "QEMU-GA" }).ProcessId
   Test-Path "C:\Program Files\Qemu-ga\qemu-ga-$PID.log"
   # Should return: True

   # View log content (optional)
   Get-Content "C:\Program Files\Qemu-ga\qemu-ga-$PID.log" | Select-Object -First 20
   ```

5. **Test that logs are being written**
   ```powershell
   # Get current log size
   $logFile = Get-ChildItem "C:\Program Files\Qemu-ga\qemu-ga-*.log"
   $initialSize = $logFile.Length

   # Wait a few seconds
   Start-Sleep -Seconds 5

   # Check if log file grew
   $logFile = Get-ChildItem "C:\Program Files\Qemu-ga\qemu-ga-*.log"
   $newSize = $logFile.Length

   if ($newSize -gt $initialSize) {
       Write-Host "✓ Logging is working! Log file grew from $initialSize to $newSize bytes"
   } else {
       Write-Host "✗ Warning: Log file did not grow - logging may not be working"
   }
   ```

### Step 5: Sysprep and Shutdown

**CRITICAL**: Sysprep generalizes the image for reuse.

1. **Run Sysprep**
   ```powershell
   # This will generalize and shutdown the VM
   C:\Windows\System32\Sysprep\sysprep.exe /generalize /oobe /shutdown
   ```

2. **Wait for shutdown** (takes 2-5 minutes)
   - VM will automatically shutdown when complete
   - Do NOT force power off

3. **Verify shutdown**
   ```bash
   # Check VM is stopped
   virsh list --all | grep windows10-update
   # Should show: shut off
   ```

### Step 6: Optimize Image (Optional but Recommended)

```bash
cd ~/windows-image-update

# Check current size
ls -lh windows-10-oadp-v1.1.qcow2

# Compress/optimize the image (takes 10-15 minutes)
qemu-img convert -O qcow2 -c -p \
  windows-10-oadp-v1.1.qcow2 \
  windows-10-oadp-v1.1-optimized.qcow2

# Replace with optimized version
mv windows-10-oadp-v1.1-optimized.qcow2 windows-10-oadp-v1.1.qcow2

# Check new size
ls -lh windows-10-oadp-v1.1.qcow2
# May be smaller due to compression
```

### Step 7: Upload to S3

```bash
cd ~/windows-image-update

# Upload new version (takes 20-30 minutes depending on internet speed)
aws s3 cp windows-10-oadp-v1.1.qcow2 \
  s3://ocpctl-binaries/windows-images/windows-10-oadp-v1.1.qcow2 \
  --storage-class STANDARD \
  --metadata version=1.1,updated=$(date +%Y-%m-%d),qemu-ga-logging=enabled

# Update "latest" symlink to point to new version
aws s3 cp s3://ocpctl-binaries/windows-images/windows-10-oadp-v1.1.qcow2 \
  s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2

# Verify upload
aws s3 ls s3://ocpctl-binaries/windows-images/ --human-readable

# Expected output:
# 2026-XX-XX XX:XX:XX   23.0 GiB windows-10-oadp.qcow2
# 2026-XX-XX XX:XX:XX   23.0 GiB windows-10-oadp-v1.1.qcow2
```

### Step 8: Update Code References

```bash
cd /Users/tsanders/Workspace2/ocpctl

# Update snapshot version in auto-setup-irsa.sh
sed -i '' 's/SNAPSHOT_VERSION="1.0"/SNAPSHOT_VERSION="1.1"/' \
  manifests/windows-vm/auto-setup-irsa.sh

# Verify change
grep 'SNAPSHOT_VERSION=' manifests/windows-vm/auto-setup-irsa.sh
# Should show: SNAPSHOT_VERSION="1.1"

# Commit changes
git add manifests/windows-vm/auto-setup-irsa.sh
git commit -m "Update Windows image to v1.1 with qemu-guest-agent logging enabled

- Enabled qemu-guest-agent verbose logging (-v flag)
- Service configured to auto-start with logging
- Logs available at: C:\\Program Files\\Qemu-ga\\qemu-ga-\$PID.log
- Updated snapshot version to 1.1
- Resolves GitHub issue #41"

# Deploy
./scripts/deploy.sh
```

### Step 9: Test New Image

Create a test cluster with Windows VMs:

```bash
# Via Web UI or CLI
# Profile: aws-virt-windows-minimal-ga
# Region: Any region (first deployment will use S3, create snapshot)
```

Verify on the Windows VM:

```powershell
# Check service
sc.exe query QEMU-GA
sc.exe qc QEMU-GA

# Verify logging enabled
$PID = (Get-WmiObject Win32_Service | Where-Object { $_.Name -eq "QEMU-GA" }).ProcessId
Test-Path "C:\Program Files\Qemu-ga\qemu-ga-$PID.log"
# Should return: True (without manual configuration!)

# View logs
Get-Content "C:\Program Files\Qemu-ga\qemu-ga-$PID.log" | Select-Object -First 50
```

---

## Cleanup

After successful upload and testing:

```bash
# Remove local copies
cd ~/windows-image-update
rm -f windows-10-oadp-v1.0.qcow2
rm -f windows-10-oadp-v1.1.qcow2

# Remove VM from libvirt (if using virt-manager)
virsh undefine windows10-update --remove-all-storage
```

---

## Troubleshooting

### VM Won't Boot

**Issue**: VM fails to start or shows black screen

**Solutions**:
- Verify KVM is enabled: `lsmod | grep kvm`
- Try different graphics option: `--graphics spice` instead of vnc
- Check logs: `virsh dumpxml windows10-update`

### Sysprep Fails

**Issue**: Sysprep shows error or fails to generalize

**Solutions**:
- Ensure Windows is fully updated first
- Check: `C:\Windows\System32\Sysprep\Panther\setuperr.log` for errors
- Common issue: Store apps - Remove via PowerShell:
  ```powershell
  Get-AppxPackage -AllUsers | Remove-AppxPackage
  ```

### qemu-guest-agent Not Found

**Issue**: `sc.exe query QEMU-GA` returns "service does not exist"

**Solutions**:
- Install qemu-guest-agent: Download from [Fedora VirtIO drivers](https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/latest-virtio/)
- Or check if service has different name:
  ```powershell
  Get-Service | Where-Object { $_.Name -like "*qemu*" -or $_.DisplayName -like "*guest*" }
  ```

### Upload Timeout

**Issue**: S3 upload times out or fails

**Solutions**:
- Use multipart upload for large files:
  ```bash
  aws configure set default.s3.multipart_threshold 64MB
  aws configure set default.s3.multipart_chunksize 16MB
  ```
- Resume interrupted upload with `--only-show-errors` flag
- Consider uploading from EC2 instance in same region for faster transfer

---

## Version History

| Version | Date | Changes | Author |
|---------|------|---------|--------|
| 1.0 | 2026-03-XX | Initial Windows 10 OADP image | Unknown |
| 1.1 | 2026-05-31 | Added qemu-guest-agent verbose logging | tsanders |

---

## Related Documentation

- [AUTOMATED-DEPLOYMENT.md](./AUTOMATED-DEPLOYMENT.md) - How Windows VMs are deployed
- [QUICKSTART-IRSA.md](./QUICKSTART-IRSA.md) - Manual IRSA setup
- [GitHub Issue #41](https://github.com/tsanders-rh/ocpctl/issues/41) - qemu-guest-agent logging request
- [GitHub Issue #40](https://github.com/tsanders-rh/ocpctl/issues/40) - Windows VM snapshot optimization

---

## Future Improvements

Consider automating image creation with:
- **Packer**: HashiCorp tool for automated image building
- **CI/CD Pipeline**: Automated testing and validation before upload
- **Multiple Variants**: Different Windows versions, different tool sets
- **Vulnerability Scanning**: Automated security scanning of images
