/**
 * IAM Auth Provider
 *
 * Handles AWS IAM authentication by delegating to server-side Next.js API routes.
 * The AWS SDK requires Node.js APIs that don't work in browsers, so all AWS
 * operations are handled server-side.
 */

interface IAMCredentials {
  accessKeyId: string;
  secretAccessKey: string;
  sessionToken?: string;
  region?: string;
}

interface CallerIdentity {
  arn: string;
  account: string;
  userId: string;
}

export class IAMAuthProvider {
  private credentials: IAMCredentials | null = null;
  private identity: CallerIdentity | null = null;

  /**
   * Set AWS IAM credentials
   */
  setCredentials(credentials: IAMCredentials): void {
    this.credentials = credentials;
    this.identity = null; // Clear cached identity
  }

  /**
   * Get current credentials
   */
  getCredentials(): IAMCredentials | null {
    return this.credentials;
  }

  /**
   * Verify IAM credentials by calling server-side API route
   */
  async verifyCredentials(): Promise<CallerIdentity> {
    if (!this.credentials) {
      throw new Error("No credentials set");
    }

    const response = await fetch("/api/auth/iam/verify", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(this.credentials),
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.message || "Failed to verify credentials");
    }

    const data = await response.json();
    this.identity = data.identity;
    return data.identity;
  }

  /**
   * Get headers for a request with AWS SigV4 signature
   */
  async getHeaders(
    method: string,
    url: string,
    body?: string
  ): Promise<HeadersInit> {
    if (!this.credentials) {
      throw new Error("No credentials set");
    }

    // Call server-side API route to sign the request
    const response = await fetch("/api/auth/iam/sign", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        ...this.credentials,
        method,
        url,
        body,
      }),
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.message || "Failed to sign request");
    }

    const data = await response.json();
    return data.headers;
  }

  /**
   * Get current identity (cached)
   */
  getIdentity(): CallerIdentity | null {
    return this.identity;
  }

  /**
   * Clear credentials and identity
   */
  clear(): void {
    this.credentials = null;
    this.identity = null;
  }

  /**
   * Refresh credentials - No-op for IAM (credentials are long-lived)
   */
  async refresh(): Promise<void> {
    // IAM credentials don't need refresh in the same way JWT does
    // If using temporary credentials (STS), the caller would need to
    // refresh them externally and call setCredentials again
    return Promise.resolve();
  }
}

export const iamAuthProvider = new IAMAuthProvider();
