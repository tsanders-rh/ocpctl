import { apiClient } from "../client";
import type { Profile, Platform } from "@/types/api";

export const profilesApi = {
  list: async (platform?: Platform): Promise<Profile[]> => {
    const query = platform ? `?platform=${platform}` : "";
    const response = await apiClient.get<{ profiles: Profile[] }>(
      `/profiles${query}`
    );
    return response.profiles;
  },

  get: async (name: string): Promise<Profile> => {
    return apiClient.get<Profile>(`/profiles/${name}`);
  },
};
