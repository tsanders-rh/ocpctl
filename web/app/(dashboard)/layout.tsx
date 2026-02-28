"use client";

import { useEffect } from "react";
import { useCurrentUser } from "@/lib/hooks/useAuth";
import { Sidebar } from "@/components/layout/Sidebar";
import { Header } from "@/components/layout/Header";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { isLoading, isError } = useCurrentUser();

  // Redirect to login if not authenticated
  useEffect(() => {
    if (isError) {
      window.location.href = "/login";
    }
  }, [isError]);

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="text-lg">Loading...</div>
      </div>
    );
  }

  return (
    <div className="flex h-screen">
      <Sidebar />
      <div className="flex flex-1 flex-col">
        <Header />
        <main className="flex-1 overflow-auto bg-gray-50 p-6">{children}</main>
      </div>
    </div>
  );
}
