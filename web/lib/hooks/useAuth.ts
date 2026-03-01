import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { authApi } from "../api";
import { useAuthStore } from "../stores/authStore";
import type {
  LoginRequest,
  UpdateMeRequest,
  ChangePasswordRequest,
} from "@/types/api";

export function useCurrentUser() {
  const { setUser, setLoading } = useAuthStore();

  return useQuery({
    queryKey: ["auth", "me"],
    queryFn: async () => {
      const user = await authApi.getMe();
      setUser(user);
      setLoading(false);
      return user;
    },
    retry: false,
    staleTime: 5 * 60 * 1000, // 5 minutes
  });
}

export function useLogin() {
  const router = useRouter();
  const { setUser, setAccessToken } = useAuthStore();

  return useMutation({
    mutationFn: (data: LoginRequest) => authApi.login(data),
    onSuccess: (response) => {
      console.log('[Login] Received response:', {
        hasUser: !!response.user,
        hasAccessToken: !!response.access_token,
        accessTokenLength: response.access_token?.length || 0,
      });

      setUser(response.user);
      setAccessToken(response.access_token);

      console.log('[Login] Token saved to store, navigating to /clusters');
      router.push("/clusters");
    },
  });
}

export function useLogout() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { logout } = useAuthStore();

  return useMutation({
    mutationFn: () => authApi.logout(),
    onSuccess: () => {
      logout();
      queryClient.clear();
      router.push("/login");
    },
    onError: () => {
      // Force logout even if API call fails
      logout();
      queryClient.clear();
      router.push("/login");
    },
  });
}

export function useUpdateProfile() {
  const queryClient = useQueryClient();
  const { setUser } = useAuthStore();

  return useMutation({
    mutationFn: (data: UpdateMeRequest) => authApi.updateMe(data),
    onSuccess: (user) => {
      setUser(user);
      queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
    },
  });
}

export function useChangePassword() {
  const { logout } = useAuthStore();
  const router = useRouter();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: ChangePasswordRequest) => authApi.changePassword(data),
    onSuccess: () => {
      // Logout after password change (all sessions revoked)
      logout();
      queryClient.clear();
      router.push("/login");
    },
  });
}
