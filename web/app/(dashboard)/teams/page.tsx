"use client";

import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/hooks/useAuth";
import { useQuery } from "@tanstack/react-query";
import { adminApi } from "@/lib/api/endpoints/admin";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Users } from "lucide-react";
import type { TeamWithCount } from "@/types/api";

export default function MyTeamsPage() {
  const router = useRouter();
  const { user } = useAuth();

  const { data: teamsData, isLoading } = useQuery({
    queryKey: ["teams"],
    queryFn: () => adminApi.listTeams(),
  });

  if (isLoading) {
    return <div className="p-8">Loading teams...</div>;
  }

  const managedTeams = user?.managed_teams || [];
  const myTeams = (teamsData?.teams || []).filter((team: TeamWithCount) =>
    managedTeams.includes(team.name)
  );

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">My Teams</h1>
        <p className="text-muted-foreground">
          Manage members of teams you administer
        </p>
      </div>

      {myTeams.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <Users className="h-12 w-12 text-muted-foreground mb-4" />
            <p className="text-lg font-medium">No teams to manage</p>
            <p className="text-sm text-muted-foreground mt-2">
              You don't have admin privileges for any teams yet
            </p>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {myTeams.map((team) => (
            <Card key={team.id} className="hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push(`/teams/${encodeURIComponent(team.name)}`)}>
              <CardHeader>
                <CardTitle>{team.name}</CardTitle>
                {team.description && (
                  <CardDescription>{team.description}</CardDescription>
                )}
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">Clusters</span>
                    <span className="font-medium">{team.cluster_count}</span>
                  </div>
                  <Button variant="outline" size="sm" className="w-full mt-4">
                    <Users className="mr-2 h-4 w-4" />
                    Manage Members
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
