import { apiClient } from "../client";
import type {
  LoginRequest,
  LoginResponse,
  User,
  UpdateMeRequest,
  ChangePasswordRequest,
} from "@/types/api";

export const authApi = {
  login: async (data: LoginRequest): Promise<LoginResponse> => {
    return apiClient.post<LoginResponse>("/auth/login", data);
  },

  logout: async (): Promise<void> => {
    return apiClient.post<void>("/auth/logout");
  },

  refresh: async (): Promise<LoginResponse> => {
    return apiClient.post<LoginResponse>("/auth/refresh");
  },

  getMe: async (): Promise<User> => {
    return apiClient.get<User>("/auth/me");
  },

  updateMe: async (data: UpdateMeRequest): Promise<User> => {
    return apiClient.patch<User>("/auth/me", data);
  },

  changePassword: async (data: ChangePasswordRequest): Promise<void> => {
    return apiClient.post<void>("/auth/password", data);
  },
};
