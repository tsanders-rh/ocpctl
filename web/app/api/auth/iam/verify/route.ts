/**
 * IAM Auth Verification API Route
 *
 * This route handles AWS IAM credential verification on the server-side.
 * It uses the AWS SDK which requires Node.js APIs not available in browsers.
 */

import { NextRequest, NextResponse } from 'next/server';
import { STSClient, GetCallerIdentityCommand } from '@aws-sdk/client-sts';

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();
    const { accessKeyId, secretAccessKey, sessionToken, region } = body;

    // Validate required fields
    if (!accessKeyId || !secretAccessKey) {
      return NextResponse.json(
        { error: 'Missing required credentials' },
        { status: 400 }
      );
    }

    // Create STS client with provided credentials
    const credentials = sessionToken
      ? {
          accessKeyId,
          secretAccessKey,
          sessionToken,
        }
      : {
          accessKeyId,
          secretAccessKey,
        };

    const stsClient = new STSClient({
      region: region || 'us-east-1',
      credentials,
    });

    // Verify credentials by calling GetCallerIdentity
    const command = new GetCallerIdentityCommand({});
    const response = await stsClient.send(command);

    // Return caller identity information
    return NextResponse.json({
      success: true,
      identity: {
        arn: response.Arn,
        account: response.Account,
        userId: response.UserId,
      },
    });
  } catch (error) {
    console.error('IAM verification error:', error);

    // Return appropriate error message
    if (error instanceof Error) {
      return NextResponse.json(
        {
          error: 'Invalid credentials',
          message: error.message,
        },
        { status: 401 }
      );
    }

    return NextResponse.json(
      { error: 'Failed to verify credentials' },
      { status: 500 }
    );
  }
}
