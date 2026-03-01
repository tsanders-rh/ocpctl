import { apiClient } from "./client";
import type { User, CreateUserRequest, UpdateUserRequest } from "@/types/api";

export const usersApi = {
  list: async (): Promise<{ users: User[]; total: number }> => {
    return apiClient.get("/users");
  },

  get: async (id: string): Promise<User> => {
    return apiClient.get(`/users/${id}`);
  },

  create: async (data: CreateUserRequest): Promise<User> => {
    return apiClient.post("/users", data);
  },

  update: async (id: string, data: UpdateUserRequest): Promise<User> => {
    return apiClient.patch(`/users/${id}`, data);
  },

  delete: async (id: string): Promise<void> => {
    return apiClient.delete(`/users/${id}`);
  },
};
