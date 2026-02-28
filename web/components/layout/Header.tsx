"use client";

import { useAuthStore } from "@/lib/stores/authStore";
import { useLogout } from "@/lib/hooks/useAuth";
import { Button } from "@/components/ui/button";
import { LogOut, User } from "lucide-react";

export function Header() {
  const { user } = useAuthStore();
  const logout = useLogout();

  return (
    <header className="flex h-16 items-center justify-between border-b bg-card px-6">
      <div className="flex items-center space-x-4">
        <h1 className="text-xl font-semibold">OpenShift Cluster Control</h1>
      </div>
      <div className="flex items-center space-x-4">
        {user && (
          <>
            <div className="flex items-center space-x-2 text-sm">
              <User className="h-4 w-4 text-muted-foreground" />
              <span className="font-medium">{user.username}</span>
              <span className="text-muted-foreground">({user.role})</span>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => logout.mutate()}
              disabled={logout.isPending}
            >
              <LogOut className="mr-2 h-4 w-4" />
              {logout.isPending ? "Logging out..." : "Logout"}
            </Button>
          </>
        )}
      </div>
    </header>
  );
}
