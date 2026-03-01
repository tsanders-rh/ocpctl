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
  const login = useLogin();
  const { authMode, setUser } = useAuthStore();

  const handleJwtSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    login.mutate({ email, password });
  };

  const handleIamLogin = async () => {
    setIamLoading(true);
    setIamError(null);

    try {
      // Verify AWS credentials
      const identity = await iamAuthProvider.verifyCredentials();

      // Make request to backend to get/create user from IAM identity
      // For now, we'll just set a placeholder user
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
          : "Failed to authenticate with AWS IAM. Please ensure AWS credentials are configured."
      );
    } finally {
      setIamLoading(false);
    }
  };

  // IAM Mode - Show AWS IAM login
  if (authMode === "iam") {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-50">
        <Card className="w-full max-w-md">
          <CardHeader className="space-y-1">
            <CardTitle className="text-3xl font-bold text-center">
              ocpctl
            </CardTitle>
            <CardDescription className="text-center">
              Sign in with AWS IAM
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="text-sm text-muted-foreground text-center">
              Authentication uses your AWS credentials configured on this machine.
            </div>
            {iamError && (
              <div className="text-sm text-red-600 bg-red-50 p-3 rounded-md">
                {iamError}
              </div>
            )}
            <Button
              onClick={handleIamLogin}
              className="w-full"
              disabled={iamLoading}
            >
              {iamLoading ? "Verifying AWS Credentials..." : "Sign in with AWS IAM"}
            </Button>
            <div className="mt-4 text-xs text-muted-foreground text-center space-y-2">
              <p>Ensure you have AWS credentials configured:</p>
              <ul className="list-disc list-inside text-left space-y-1">
                <li>Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)</li>
                <li>AWS credentials file (~/.aws/credentials)</li>
                <li>EC2 instance role (if running on EC2)</li>
              </ul>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  // JWT Mode - Show email/password login
  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50">
      <Card className="w-full max-w-md">
        <CardHeader className="space-y-1">
          <CardTitle className="text-3xl font-bold text-center">
            ocpctl
          </CardTitle>
          <CardDescription className="text-center">
            OpenShift Cluster Control
          </CardDescription>
        </CardHeader>
        <CardContent>
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
        </CardContent>
      </Card>
    </div>
  );
}
