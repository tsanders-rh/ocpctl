"use client";

import { useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Copy, Download, ExternalLink, Clock, Check } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { apiClient } from "@/lib/api/client";

interface LeaseCredentialsModalProps {
  isOpen: boolean;
  onClose: () => void;
  clusterName: string;
  clusterId: string;
  leaseExpiresAt: string;
  credentials: {
    sa_token?: string;
    oc_login_command?: string;
    kubeconfig_s3_uri?: string;
    api_url?: string;
    console_url?: string;
  };
}

export function LeaseCredentialsModal({
  isOpen,
  onClose,
  clusterName,
  clusterId,
  leaseExpiresAt,
  credentials,
}: LeaseCredentialsModalProps) {
  const [copiedToken, setCopiedToken] = useState(false);
  const [copiedCommand, setCopiedCommand] = useState(false);

  const copyToClipboard = async (text: string, setCopied: (val: boolean) => void) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      toast.success("Copied to clipboard");
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      toast.error("Failed to copy to clipboard");
    }
  };

  const handleDownloadKubeconfig = async () => {
    if (!credentials.kubeconfig_s3_uri) {
      toast.error("Kubeconfig not available");
      return;
    }

    try {
      // Call API to get presigned URL for kubeconfig download
      const data = await apiClient.get<{ download_url: string }>(`/clusters/${clusterId}/kubeconfig/download-url`);
      const downloadUrl = data.download_url;

      // Trigger download
      const link = document.createElement("a");
      link.href = downloadUrl;
      link.download = `kubeconfig-${clusterName}`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);

      toast.success("Kubeconfig downloaded");
    } catch (err) {
      toast.error("Failed to download kubeconfig");
    }
  };

  const expiresAt = new Date(leaseExpiresAt);
  const timeRemaining = formatDistanceToNow(expiresAt, { addSuffix: false });

  const maskToken = (token: string) => {
    if (!token || token.length < 20) return token;
    return `${token.substring(0, 12)}...${token.substring(token.length - 8)}`;
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-2xl">Cluster Leased Successfully</DialogTitle>
          <DialogDescription>
            Access credentials for your cluster
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6">
          {/* Cluster Info */}
          <div className="flex items-center justify-between p-4 bg-green-50 border border-green-200 rounded-lg">
            <div>
              <p className="font-semibold text-lg">{clusterName}</p>
              <p className="text-sm text-muted-foreground">Cluster ID: {clusterId}</p>
            </div>
            <div className="text-right">
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Clock className="h-4 w-4" />
                <span>Expires in {timeRemaining}</span>
              </div>
              <p className="text-xs text-muted-foreground mt-1">
                {expiresAt.toLocaleString()}
              </p>
            </div>
          </div>

          {/* Access Token */}
          {credentials.sa_token && (
            <Card className="p-4">
              <div className="flex items-start justify-between mb-2">
                <div>
                  <h3 className="font-semibold flex items-center gap-2">
                    Access Token
                  </h3>
                  <p className="text-sm text-muted-foreground mt-1">
                    Use this token to authenticate with the cluster
                  </p>
                </div>
              </div>
              <div className="mt-3">
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-gray-100 px-3 py-2 rounded text-sm font-mono break-all">
                    {maskToken(credentials.sa_token)}
                  </code>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => copyToClipboard(credentials.sa_token!, setCopiedToken)}
                  >
                    {copiedToken ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                  </Button>
                </div>
                <div className="flex items-center gap-2 mt-2">
                  <Badge variant="destructive" className="text-xs">
                    ⚠️ Token expires with lease
                  </Badge>
                </div>
              </div>
            </Card>
          )}

          {/* Kubeconfig Download */}
          {credentials.kubeconfig_s3_uri && (
            <Card className="p-4">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="font-semibold flex items-center gap-2">
                    Kubeconfig File
                  </h3>
                  <p className="text-sm text-muted-foreground mt-1">
                    Token-based kubeconfig file for kubectl/oc
                  </p>
                </div>
                <Button onClick={handleDownloadKubeconfig} variant="outline">
                  <Download className="h-4 w-4 mr-2" />
                  Download
                </Button>
              </div>
              <p className="text-xs text-muted-foreground mt-3">
                This kubeconfig contains the access token and will expire when the lease ends
              </p>
            </Card>
          )}

          {/* CLI Login Command */}
          {credentials.oc_login_command && (
            <Card className="p-4">
              <div>
                <h3 className="font-semibold flex items-center gap-2 mb-2">
                  CLI Login Command
                </h3>
                <p className="text-sm text-muted-foreground mb-3">
                  Copy and paste this command to login via oc/kubectl
                </p>
              </div>
              <div className="flex items-center gap-2">
                <code className="flex-1 bg-gray-100 px-3 py-2 rounded text-sm font-mono break-all">
                  {credentials.oc_login_command}
                </code>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => copyToClipboard(credentials.oc_login_command!, setCopiedCommand)}
                >
                  {copiedCommand ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                </Button>
              </div>
            </Card>
          )}

          {/* Web Console */}
          {credentials.console_url && (
            <Card className="p-4">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="font-semibold flex items-center gap-2">
                    Web Console
                  </h3>
                  <p className="text-sm text-muted-foreground mt-1">
                    Access the cluster's web console
                  </p>
                </div>
                <a
                  href={credentials.console_url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <Button variant="outline">
                    <ExternalLink className="h-4 w-4 mr-2" />
                    Open Console
                  </Button>
                </a>
              </div>
              <p className="text-xs text-muted-foreground mt-3">
                Log in using the access token above
              </p>
            </Card>
          )}

          {/* Instructions */}
          <Card className="p-4 bg-blue-50 border-blue-200">
            <h3 className="font-semibold mb-2">Important Notes</h3>
            <ul className="text-sm space-y-1 text-muted-foreground list-disc list-inside">
              <li>All credentials automatically expire when the lease ends</li>
              <li>You can view cluster details and manage the lease from the cluster page</li>
              <li>The cluster will be automatically returned to the pool after the lease expires</li>
              <li>You can manually release the cluster when you're done using it</li>
            </ul>
          </Card>

          {/* Actions */}
          <div className="flex justify-end gap-3 pt-4 border-t">
            <Button
              variant="outline"
              onClick={onClose}
            >
              Close
            </Button>
            <a href={`/clusters/${clusterId}`}>
              <Button>
                View Cluster Details
              </Button>
            </a>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
