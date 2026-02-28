# ocpctl Web Frontend

Next.js 14 web frontend for ocpctl (OpenShift Cluster Control).

## Features

- **Dual Authentication**: Supports both JWT (email/password) and AWS IAM authentication
- **Cluster Management**: Create, view, delete, and extend cluster lifetimes
- **Profile Browser**: Browse and compare cluster profiles
- **Real-time Updates**: Auto-polling for cluster status changes
- **Responsive UI**: Modern design with Tailwind CSS and Shadcn/ui components
- **Type-safe**: End-to-end TypeScript with Zod validation

## Tech Stack

- **Next.js 14+** with App Router
- **TypeScript** for type safety
- **Tailwind CSS** for styling
- **Shadcn/ui** for UI components
- **React Hook Form + Zod** for form validation
- **TanStack Query** for server state management
- **Zustand** for auth state management

## Development

### Prerequisites

- Node.js 18+ and npm
- Running ocpctl API server (default: http://localhost:8080)

### Setup

```bash
# Install dependencies
npm install

# Copy environment file
cp .env.local.example .env.local

# Edit .env.local with your configuration
# NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1
# NEXT_PUBLIC_AUTH_MODE=jwt

# Run development server
npm run dev
```

The app will be available at http://localhost:3000

### Default Login (JWT mode)

- **Email**: admin@localhost
- **Password**: changeme

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEXT_PUBLIC_API_URL` | API server URL | `http://localhost:8080/api/v1` |
| `NEXT_PUBLIC_AUTH_MODE` | Authentication mode (`jwt` or `iam`) | `jwt` |
| `NEXT_PUBLIC_AWS_REGION` | AWS region (for IAM mode) | `us-east-1` |

## Building for Production

```bash
# Build the production bundle
npm run build

# Start production server
npm start
```

## Project Structure

```
web/
├── app/
│   ├── (auth)/              # Authentication pages
│   │   └── login/
│   ├── (dashboard)/         # Main dashboard
│   │   ├── clusters/        # Cluster management
│   │   │   ├── page.tsx     # Cluster list
│   │   │   ├── new/         # Create cluster
│   │   │   └── [id]/        # Cluster detail
│   │   └── profiles/        # Profile browser
│   ├── layout.tsx           # Root layout
│   ├── providers.tsx        # React Query provider
│   └── globals.css
├── components/
│   ├── ui/                  # Shadcn/ui components
│   ├── clusters/            # Cluster-specific components
│   └── layout/              # Header, Sidebar
├── lib/
│   ├── api/                 # API client
│   │   ├── client.ts        # Core API client
│   │   └── endpoints/       # API endpoints
│   ├── hooks/               # React Query hooks
│   ├── stores/              # Zustand stores
│   ├── schemas/             # Zod validation schemas
│   └── utils/               # Utility functions
├── types/
│   └── api.ts               # TypeScript type definitions
└── middleware.ts            # Route protection
```

## Authentication Modes

### JWT Mode (Default)

Traditional email/password authentication with access tokens and refresh tokens.

```bash
NEXT_PUBLIC_AUTH_MODE=jwt
```

- Login page with email/password form
- Access tokens stored in memory
- Refresh tokens in HTTP-only cookies
- Automatic token refresh on 401 responses

### IAM Mode

AWS IAM authentication using AWS credentials (for AWS deployments).

```bash
NEXT_PUBLIC_AUTH_MODE=iam
NEXT_PUBLIC_AWS_REGION=us-east-1
```

- Uses AWS SDK credential provider
- Requests signed with AWS SigV4
- Supports EC2 instance roles

## API Client

The API client (`lib/api/client.ts`) automatically:
- Adds authentication headers (JWT Bearer token or IAM signature)
- Refreshes access tokens on 401 responses
- Handles errors with typed error responses
- Provides type-safe endpoint methods

## State Management

- **Auth State**: Zustand store (`lib/stores/authStore.ts`)
- **Server State**: TanStack Query with automatic caching and polling
- **Form State**: React Hook Form with Zod validation

## Development Commands

```bash
# Start dev server
npm run dev

# Type checking
npm run type-check

# Linting
npm run lint

# Build for production
npm run build

# Start production server
npm start
```

## Deployment

See [Deployment Guide](../docs/DEPLOYMENT_WEB.md) for production deployment instructions.

## License

MIT
