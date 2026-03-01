/**
 * IAM Auth Provider - Placeholder Implementation
 *
 * IMPORTANT: Full AWS IAM authentication requires server-side processing
 * because AWS SDK modules use Node.js APIs that don't work in browsers.
 *
 * To implement IAM auth properly:
 * 1. Create Next.js API routes (app/api/auth/iam/*.ts)
 * 2. Use AWS SDK on the server-side to verify credentials
 * 3. Sign requests with SigV4 on the backend
 * 4. Return a session token/JWT to the browser
 *
 * For now, this is a stub that would need to be replaced with
 * server-side API route handlers.
 */
export class IAMAuthProvider {
  /**
   * Get headers for a request
   * Currently just marks the request as using IAM mode
   */
  async getHeaders(): Promise<HeadersInit> {
    return {
      "Content-Type": "application/json",
      "X-Auth-Mode": "iam",
    };
  }

  /**
   * Verify IAM credentials - STUB
   * In production, this would call a Next.js API route that
   * uses AWS SDK server-side to verify credentials
   */
  async verifyCredentials(): Promise<{
    arn: string;
    account: string;
    userId: string;
  }> {
    throw new Error(
      "IAM authentication is not fully implemented. " +
        "AWS SDK requires server-side processing. " +
        "Please use JWT authentication mode or implement server-side IAM auth via Next.js API routes."
    );
  }

  /**
   * Refresh credentials - STUB
   */
  async refresh(): Promise<void> {
    return Promise.resolve();
  }
}

export const iamAuthProvider = new IAMAuthProvider();
