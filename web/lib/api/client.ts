import { useAuthStore } from "../stores/authStore";
import { iamAuthProvider } from "./auth-iam";
import type { APIError } from "@/types/api";

export class ApiError extends Error {
  constructor(
    public statusCode: number,
    message: string,
    public response?: any
  ) {
    super(message);
    this.name = "ApiError";
  }
}

class ApiClient {
  private baseURL: string;
  private refreshPromise: Promise<void> | null = null;

  constructor() {
    this.baseURL =
      process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api/v1";
  }

  private async getHeaders(): Promise<HeadersInit> {
    const { accessToken, authMode } = useAuthStore.getState();

    // IAM auth mode - use AWS credentials
    if (authMode === "iam") {
      return await iamAuthProvider.getHeaders();
    }

    // JWT auth mode - use access token
    const headers: HeadersInit = {
      "Content-Type": "application/json",
    };

    if (accessToken) {
      headers["Authorization"] = `Bearer ${accessToken}`;
    }

    return headers;
  }

  private async refreshToken(): Promise<void> {
    const { authMode } = useAuthStore.getState();

    // IAM mode - refresh AWS credentials
    if (authMode === "iam") {
      await iamAuthProvider.refresh();
      return;
    }

    // JWT mode - refresh access token
    // Prevent multiple simultaneous refresh requests
    if (this.refreshPromise) {
      return this.refreshPromise;
    }

    this.refreshPromise = (async () => {
      try {
        const response = await fetch(`${this.baseURL}/auth/refresh`, {
          method: "POST",
          credentials: "include",
        });

        if (!response.ok) {
          // Refresh failed, logout user
          useAuthStore.getState().logout();
          if (typeof window !== "undefined") {
            window.location.href = "/login";
          }
          throw new Error("Session expired");
        }

        const data = await response.json();
        useAuthStore.getState().setAccessToken(data.access_token);
        useAuthStore.getState().setUser(data.user);
      } finally {
        this.refreshPromise = null;
      }
    })();

    return this.refreshPromise;
  }

  async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers = await this.getHeaders();
    const url = endpoint.startsWith("http")
      ? endpoint
      : `${this.baseURL}${endpoint}`;

    const config: RequestInit = {
      ...options,
      headers: {
        ...headers,
        ...options.headers,
      },
      credentials: "include",
    };

    let response = await fetch(url, config);

    // Handle 401 Unauthorized - try to refresh token
    if (response.status === 401 && endpoint !== "/auth/refresh") {
      await this.refreshToken();

      // Retry the request with new token
      const newHeaders = await this.getHeaders();
      config.headers = {
        ...newHeaders,
        ...options.headers,
      };
      response = await fetch(url, config);
    }

    if (!response.ok) {
      let error: APIError;
      try {
        error = await response.json();
      } catch {
        error = {
          error: "Unknown Error",
          message: response.statusText,
          status_code: response.status,
        };
      }

      throw new ApiError(
        response.status,
        error.message || "Request failed",
        error
      );
    }

    // Handle 204 No Content
    if (response.status === 204) {
      return {} as T;
    }

    return response.json();
  }

  async get<T>(endpoint: string): Promise<T> {
    return this.request<T>(endpoint, { method: "GET" });
  }

  async post<T>(endpoint: string, data?: any): Promise<T> {
    return this.request<T>(endpoint, {
      method: "POST",
      body: data ? JSON.stringify(data) : undefined,
    });
  }

  async put<T>(endpoint: string, data?: any): Promise<T> {
    return this.request<T>(endpoint, {
      method: "PUT",
      body: data ? JSON.stringify(data) : undefined,
    });
  }

  async patch<T>(endpoint: string, data?: any): Promise<T> {
    return this.request<T>(endpoint, {
      method: "PATCH",
      body: data ? JSON.stringify(data) : undefined,
    });
  }

  async delete<T>(endpoint: string): Promise<T> {
    return this.request<T>(endpoint, { method: "DELETE" });
  }
}

export const apiClient = new ApiClient();
