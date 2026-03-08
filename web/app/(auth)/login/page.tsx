"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useLogin } from "@/lib/hooks/useAuth";
import { useAuthStore } from "@/lib/stores/authStore";
import { iamAuthProvider } from "@/lib/api/auth-iam";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [iamError, setIamError] = useState<string | null>(null);
  const [iamLoading, setIamLoading] = useState(false);
  const [awsAccessKeyId, setAwsAccessKeyId] = useState("");
  const [awsSecretAccessKey, setAwsSecretAccessKey] = useState("");
  const [awsSessionToken, setAwsSessionToken] = useState("");
  const [awsRegion, setAwsRegion] = useState("us-east-1");
  const [showIamLogin, setShowIamLogin] = useState(false);
  const login = useLogin();
  const { setUser } = useAuthStore();

  const handleJwtSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    login.mutate({ email, password });
  };

  const handleIamSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIamLoading(true);
    setIamError(null);

    try {
      // Set credentials in IAM provider
      iamAuthProvider.setCredentials({
        accessKeyId: awsAccessKeyId,
        secretAccessKey: awsSecretAccessKey,
        sessionToken: awsSessionToken || undefined,
        region: awsRegion,
      });

      // Verify credentials via backend
      const identity = await iamAuthProvider.verifyCredentials();

      // Set user in store (backend will auto-provision on first API call)
      setUser({
        id: identity.userId,
        email: `${identity.arn}`,
        username: identity.arn.split("/").pop() || identity.userId,
        role: "USER" as any,
        active: true,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      });

      router.push("/clusters");
    } catch (error) {
      setIamError(
        error instanceof Error
          ? error.message
          : "Failed to authenticate with AWS IAM. Please check your credentials."
      );
    } finally {
      setIamLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50">
      <Card className="w-full max-w-md">
        <CardHeader className="space-y-1">
          <CardTitle className="text-3xl font-bold text-center">
            ocpctl
          </CardTitle>
          <CardDescription className="text-center">
            {showIamLogin ? "Sign in with AWS IAM" : "OpenShift Cluster Control"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {!showIamLogin ? (
            // JWT Login Form
            <>
              <form onSubmit={handleJwtSubmit} className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="email">Email</Label>
                  <Input
                    id="email"
                    type="email"
                    placeholder="admin@example.com"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    required
                    autoComplete="email"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="password">Password</Label>
                  <Input
                    id="password"
                    type="password"
                    placeholder="••••••••"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                    autoComplete="current-password"
                  />
                </div>
                {login.isError && (
                  <div className="text-sm text-red-600 bg-red-50 p-3 rounded-md">
                    {login.error instanceof Error
                      ? login.error.message
                      : "Invalid email or password"}
                  </div>
                )}
                <Button
                  type="submit"
                  className="w-full"
                  disabled={login.isPending}
                >
                  {login.isPending ? "Signing in..." : "Sign in"}
                </Button>
              </form>
              <div className="mt-4 text-center text-sm text-muted-foreground">
                Default credentials: admin@example.com / changeme
              </div>
              <div className="mt-4 text-center">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowIamLogin(true)}
                  className="text-sm"
                >
                  Or sign in with AWS IAM →
                </Button>
              </div>
            </>
          ) : (
            // IAM Login Form
            <>
              <form onSubmit={handleIamSubmit} className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="accessKeyId">AWS Access Key ID</Label>
                  <Input
                    id="accessKeyId"
                    type="text"
                    placeholder="AKIA..."
                    value={awsAccessKeyId}
                    onChange={(e) => setAwsAccessKeyId(e.target.value)}
                    required
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="secretAccessKey">AWS Secret Access Key</Label>
                  <Input
                    id="secretAccessKey"
                    type="password"
                    placeholder="••••••••"
                    value={awsSecretAccessKey}
                    onChange={(e) => setAwsSecretAccessKey(e.target.value)}
                    required
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="sessionToken">Session Token (Optional)</Label>
                  <Input
                    id="sessionToken"
                    type="password"
                    placeholder="For temporary credentials"
                    value={awsSessionToken}
                    onChange={(e) => setAwsSessionToken(e.target.value)}
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="region">AWS Region</Label>
                  <Input
                    id="region"
                    type="text"
                    placeholder="us-east-1"
                    value={awsRegion}
                    onChange={(e) => setAwsRegion(e.target.value)}
                    required
                    autoComplete="off"
                  />
                </div>
                {iamError && (
                  <div className="text-sm text-red-600 bg-red-50 p-3 rounded-md">
                    {iamError}
                  </div>
                )}
                <Button
                  type="submit"
                  className="w-full"
                  disabled={iamLoading}
                >
                  {iamLoading ? "Verifying AWS Credentials..." : "Sign in with AWS IAM"}
                </Button>
              </form>
              <div className="mt-4 text-xs text-muted-foreground text-center">
                Your AWS credentials are only used for authentication and are not stored.
              </div>
              <div className="mt-4 text-center">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowIamLogin(false)}
                  className="text-sm"
                >
                  ← Back to email/password login
                </Button>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
