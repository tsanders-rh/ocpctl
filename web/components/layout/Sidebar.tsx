"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils/cn";
import { Layers, FileCode, Home, Shield, Users, BookOpen, BookText, Clock, PackageCheck, UsersRound } from "lucide-react";
import { useAuthStore } from "@/lib/stores/authStore";
import { UserRole } from "@/types/api";

const navigation = [
  { name: "Clusters", href: "/clusters", icon: Layers },
  { name: "Profiles", href: "/profiles", icon: FileCode },
  { name: "User Guide", href: "/docs", icon: BookText },
];

const externalNavigation = [
  {
    name: "API Documentation",
    href: "/swagger/index.html",
    icon: BookOpen,
    external: true,
  },
];

const adminNavigation = [
  { name: "Admin Dashboard", href: "/admin", icon: Shield },
  { name: "User Management", href: "/admin/users", icon: Users },
  { name: "Team Management", href: "/admin/teams", icon: UsersRound },
  { name: "Profile Updates", href: "/admin/profile-updates", icon: PackageCheck },
  { name: "Long-Running Clusters", href: "/admin/long-running-clusters", icon: Clock },
];

export function Sidebar() {
  const pathname = usePathname();
  const { user } = useAuthStore();
  const isAdmin = user?.role === UserRole.ADMIN;
  const isTeamAdmin = user?.role === UserRole.TEAM_ADMIN && (user?.managed_teams?.length || 0) > 0;

  return (
    <div className="flex h-screen w-64 flex-col border-r bg-card">
      <div className="flex h-16 items-center border-b px-6">
        <Link href="/" className="flex items-center space-x-2">
          <Home className="h-6 w-6" />
          <span className="text-xl font-bold">ocpctl</span>
        </Link>
      </div>
      <nav className="flex-1 space-y-1 px-3 py-4">
        {navigation.map((item) => {
          const isActive = pathname.startsWith(item.href) && !pathname.startsWith("/admin") && !pathname.startsWith("/teams");
          return (
            <Link
              key={item.name}
              href={item.href}
              className={cn(
                "group flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors",
                isActive
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )}
            >
              <item.icon
                className={cn(
                  "mr-3 h-5 w-5 flex-shrink-0",
                  isActive ? "text-primary-foreground" : "text-muted-foreground"
                )}
              />
              {item.name}
            </Link>
          );
        })}

        {isTeamAdmin && (
          <Link
            href="/teams"
            className={cn(
              "group flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors",
              pathname.startsWith("/teams")
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            )}
          >
            <UsersRound
              className={cn(
                "mr-3 h-5 w-5 flex-shrink-0",
                pathname.startsWith("/teams") ? "text-primary-foreground" : "text-muted-foreground"
              )}
            />
            My Teams
          </Link>
        )}

        {externalNavigation.map((item) => (
          <a
            key={item.name}
            href={item.href}
            target="_blank"
            rel="noopener noreferrer"
            className="group flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          >
            <item.icon className="mr-3 h-5 w-5 flex-shrink-0 text-muted-foreground" />
            {item.name}
          </a>
        ))}

        {isAdmin && (
          <>
            <div className="px-3 pt-4 pb-2">
              <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                Administration
              </p>
            </div>
            {adminNavigation.map((item) => {
              const isActive = pathname === item.href ||
                (item.href !== "/admin" && pathname.startsWith(item.href));
              return (
                <Link
                  key={item.name}
                  href={item.href}
                  className={cn(
                    "group flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors",
                    isActive
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                  )}
                >
                  <item.icon
                    className={cn(
                      "mr-3 h-5 w-5 flex-shrink-0",
                      isActive ? "text-primary-foreground" : "text-muted-foreground"
                    )}
                  />
                  {item.name}
                </Link>
              );
            })}
          </>
        )}
      </nav>
    </div>
  );
}
