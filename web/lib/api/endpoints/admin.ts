import { apiClient } from "../client";
import type {
  ListTeamsResponse,
  Team,
  CreateTeamRequest,
  UpdateTeamRequest,
  ListTeamAdminsResponse,
  GrantTeamAdminRequest,
  ListTeamMembersResponse,
  AddUserToTeamRequest,
} from "@/types/api";

export interface ClusterStatistics {
  total_clusters: number;
  active_clusters: number;
  clusters_by_status: Array<{
    status: string;
    count: number;
  }>;
  clusters_by_profile: Array<{
    profile: string;
    count: number;
  }>;
  clusters_by_platform: Array<{
    platform: string;
    count: number;
  }>;
  total_hourly_cost: number;
  total_daily_cost: number;
  total_monthly_cost: number;
  cost_by_profile: Array<{
    profile: string;
    cluster_count: number;
    hourly_cost: number;
    daily_cost: number;
    monthly_cost: number;
  }>;
  cost_by_user: Array<{
    user_id: string;
    username: string;
    cluster_count: number;
    hourly_cost: number;
    daily_cost: number;
    monthly_cost: number;
  }>;
}

export interface WorkerInfo {
  instance_id: string;
  private_ip: string;
  public_ip?: string;
  launch_time: string;
  state: string;
  health_status: string;
  type: string;
  version?: string;
}

export interface ASGInfo {
  name: string;
  desired_capacity: number;
  min_size: number;
  max_size: number;
  instances: WorkerInfo[];
}

export interface InfrastructureInfo {
  api_server: {
    ip: string;
    version: string;
    status: string;
    uptime?: string;
  };
  database: {
    host: string;
    status: string;
  };
  static_workers: WorkerInfo[];
  autoscale_group?: ASGInfo;
  timestamp: string;
}

export interface LongRunningCluster {
  id: string;
  name: string;
  platform: string;
  profile: string;
  region: string;
  owner: string;
  owner_id: string;
  status: string;
  created_at: string;
  updated_at: string;
  running_duration_hours: number;
  last_hibernated_at?: string;
  work_hours_enabled: boolean | null;
  hourly_cost: number;
  daily_cost: number;
  monthly_cost: number;
}

export interface LongRunningClustersResponse {
  clusters: LongRunningCluster[];
  total_count: number;
  min_hours: number;
  total_hourly_cost: number;
  total_daily_cost: number;
  total_monthly_cost: number;
}

export const adminApi = {
  getClusterStatistics: async (): Promise<ClusterStatistics> => {
    return apiClient.get<ClusterStatistics>("/admin/clusters/statistics");
  },
  getInfrastructure: async (): Promise<InfrastructureInfo> => {
    return apiClient.get<InfrastructureInfo>("/admin/system/infrastructure");
  },
  getLongRunningClusters: async (minHours: number = 24): Promise<LongRunningClustersResponse> => {
    return apiClient.get<LongRunningClustersResponse>(
      `/admin/clusters/long-running?min_hours=${minHours}`
    );
  },

  // Team Management
  listTeams: async (): Promise<ListTeamsResponse> => {
    return apiClient.get<ListTeamsResponse>("/admin/teams");
  },
  getTeam: async (name: string): Promise<Team> => {
    return apiClient.get<Team>(`/admin/teams/${encodeURIComponent(name)}`);
  },
  createTeam: async (data: CreateTeamRequest): Promise<Team> => {
    return apiClient.post<Team>("/admin/teams", data);
  },
  updateTeam: async (name: string, data: UpdateTeamRequest): Promise<Team> => {
    return apiClient.patch<Team>(`/admin/teams/${encodeURIComponent(name)}`, data);
  },
  deleteTeam: async (name: string): Promise<void> => {
    return apiClient.delete<void>(`/admin/teams/${encodeURIComponent(name)}`);
  },

  // Team Admin Management
  listTeamAdmins: async (teamName: string): Promise<ListTeamAdminsResponse> => {
    return apiClient.get<ListTeamAdminsResponse>(
      `/admin/teams/${encodeURIComponent(teamName)}/admins`
    );
  },
  grantTeamAdmin: async (teamName: string, data: GrantTeamAdminRequest): Promise<void> => {
    return apiClient.post<void>(
      `/admin/teams/${encodeURIComponent(teamName)}/admins`,
      data
    );
  },
  revokeTeamAdmin: async (teamName: string, userId: string): Promise<void> => {
    return apiClient.delete<void>(
      `/admin/teams/${encodeURIComponent(teamName)}/admins/${userId}`
    );
  },

  // Team Membership Management
  listTeamMembers: async (teamName: string): Promise<ListTeamMembersResponse> => {
    return apiClient.get<ListTeamMembersResponse>(
      `/admin/teams/${encodeURIComponent(teamName)}/members`
    );
  },
  getEligibleUsers: async (teamName: string): Promise<{ users: User[] }> => {
    return apiClient.get<{ users: User[] }>(
      `/admin/teams/${encodeURIComponent(teamName)}/eligible-users`
    );
  },
  addUserToTeam: async (teamName: string, data: AddUserToTeamRequest): Promise<void> => {
    return apiClient.post<void>(
      `/admin/teams/${encodeURIComponent(teamName)}/members`,
      data
    );
  },
  removeUserFromTeam: async (teamName: string, userId: string): Promise<void> => {
    return apiClient.delete<void>(
      `/admin/teams/${encodeURIComponent(teamName)}/members/${userId}`
    );
  },
};
