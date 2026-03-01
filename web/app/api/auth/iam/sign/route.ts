/**
 * IAM Auth Request Signing API Route
 *
 * This route handles AWS SigV4 request signing on the server-side.
 * It uses the AWS SDK to sign requests to the ocpctl API backend.
 */

import { NextRequest, NextResponse } from 'next/server';
import { SignatureV4 } from '@smithy/signature-v4';
import { HttpRequest } from '@smithy/protocol-http';
import { Sha256 } from '@aws-crypto/sha256-js';

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();
    const {
      accessKeyId,
      secretAccessKey,
      sessionToken,
      region,
      method,
      url,
      headers,
      body: requestBody,
    } = body;

    // Validate required fields
    if (!accessKeyId || !secretAccessKey || !method || !url) {
      return NextResponse.json(
        { error: 'Missing required fields' },
        { status: 400 }
      );
    }

    // Parse URL
    const parsedUrl = new URL(url);

    // Create credentials
    const credentials = {
      accessKeyId,
      secretAccessKey,
      ...(sessionToken && { sessionToken }),
    };

    // Create HTTP request
    const httpRequest = new HttpRequest({
      method,
      protocol: parsedUrl.protocol,
      hostname: parsedUrl.hostname,
      port: parsedUrl.port ? parseInt(parsedUrl.port) : undefined,
      path: parsedUrl.pathname + parsedUrl.search,
      headers: {
        host: parsedUrl.hostname,
        ...headers,
      },
      body: requestBody,
    });

    // Create SigV4 signer
    const signer = new SignatureV4({
      credentials,
      region: region || 'us-east-1',
      service: 'execute-api', // AWS API Gateway service
      sha256: Sha256,
    });

    // Sign the request
    const signedRequest = await signer.sign(httpRequest);

    // Return signed headers
    return NextResponse.json({
      success: true,
      headers: signedRequest.headers,
      url: url,
    });
  } catch (error) {
    console.error('Request signing error:', error);

    if (error instanceof Error) {
      return NextResponse.json(
        {
          error: 'Failed to sign request',
          message: error.message,
        },
        { status: 500 }
      );
    }

    return NextResponse.json(
      { error: 'Failed to sign request' },
      { status: 500 }
    );
  }
}
