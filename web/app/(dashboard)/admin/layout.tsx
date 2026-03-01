"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/stores/authStore";
import { UserRole } from "@/types/api";

export default function AdminLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const { user, isLoading } = useAuthStore();

  useEffect(() => {
    if (!isLoading && user && user.role !== UserRole.ADMIN) {
      // Redirect non-admin users to clusters page
      router.push("/clusters");
    }
  }, [user, isLoading, router]);

  // Show loading state while checking auth
  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="text-lg">Loading...</div>
      </div>
    );
  }

  // Show access denied for non-admin users
  if (user && user.role !== UserRole.ADMIN) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="text-center space-y-4">
          <h1 className="text-2xl font-bold text-red-600">Access Denied</h1>
          <p className="text-muted-foreground">
            You do not have permission to access this page.
          </p>
          <p className="text-sm text-muted-foreground">
            Administrator access is required.
          </p>
        </div>
      </div>
    );
  }

  return <>{children}</>;
}
