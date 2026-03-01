import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

export function middleware(request: NextRequest) {
  const authMode = process.env.NEXT_PUBLIC_AUTH_MODE || "jwt";
  const isAuthPage = request.nextUrl.pathname.startsWith("/login");

  // JWT mode: check refresh token cookie
  if (authMode === "jwt") {
    const hasRefreshToken = request.cookies.has("refresh_token");

    // Redirect unauthenticated users to login
    if (!hasRefreshToken && !isAuthPage) {
      return NextResponse.redirect(new URL("/login", request.url));
    }

    // Allow authenticated users to visit login page
    // (they may want to logout or switch accounts)
  }

  // IAM mode would check for AWS credentials
  // For now, we'll allow all requests in IAM mode

  return NextResponse.next();
}

export const config = {
  matcher: ["/((?!api|_next/static|_next/image|favicon.ico).*)"],
};
