'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Checkbox } from '@/components/ui/checkbox'
import { Separator } from '@/components/ui/separator'
import { toast } from 'sonner'
import {
  RefreshCw,
  Check,
  X,
  AlertCircle,
  Loader2,
  Package,
  ArrowUpCircle,
  Clock,
  Undo2
} from 'lucide-react'

interface ProfileVersionStatus {
  profile_name: string
  current_versions: string[]
  available_versions: string[]
  new_versions: string[]
  update_count: number
  last_checked: string
}

interface CheckVersionsResponse {
  profiles_with_updates: ProfileVersionStatus[]
  total_profiles: number
  updates_available: number
  cache_age: string
  last_checked: string
}

interface UpdateVersionsRequest {
  openshift_versions?: string[]
  kubernetes_versions?: string[]
  dry_run?: boolean
}

export default function ProfileUpdatesPage() {
  const queryClient = useQueryClient()
  const [selectedVersions, setSelectedVersions] = useState<Record<string, string[]>>({})
  const [dryRunMode, setDryRunMode] = useState(false)

  // Fetch version check data
  const { data, isLoading, refetch } = useQuery<CheckVersionsResponse>({
    queryKey: ['admin', 'profile-updates'],
    queryFn: async () => {
      const response = await fetch('/api/v1/admin/profiles/version-check', {
        credentials: 'include',
      })
      if (!response.ok) {
        throw new Error('Failed to check profile versions')
      }
      return response.json()
    },
  })

  // Mutation for updating profile versions
  const updateMutation = useMutation({
    mutationFn: async ({ profileName, versions }: { profileName: string; versions: UpdateVersionsRequest }) => {
      const response = await fetch(`/api/v1/admin/profiles/${profileName}/update-versions`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        credentials: 'include',
        body: JSON.stringify(versions),
      })
      if (!response.ok) {
        const error = await response.json()
        throw new Error(error.message || 'Failed to update profile')
      }
      return response.json()
    },
    onSuccess: (data, variables) => {
      if (data.dry_run) {
        toast.success('Dry run successful - validation passed')
      } else {
        toast.success(`Profile ${variables.profileName} updated successfully`)
        queryClient.invalidateQueries({ queryKey: ['admin', 'profile-updates'] })
        // Clear selected versions for this profile
        setSelectedVersions(prev => {
          const newState = { ...prev }
          delete newState[variables.profileName]
          return newState
        })
      }
    },
    onError: (error: Error) => {
      toast.error(`Update failed: ${error.message}`)
    },
  })

  // Mutation for rollback
  const rollbackMutation = useMutation({
    mutationFn: async (profileName: string) => {
      const response = await fetch(`/api/v1/admin/profiles/${profileName}/rollback`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!response.ok) {
        throw new Error('Failed to rollback profile')
      }
      return response.json()
    },
    onSuccess: (_, profileName) => {
      toast.success(`Profile ${profileName} rolled back to previous version`)
      queryClient.invalidateQueries({ queryKey: ['admin', 'profile-updates'] })
    },
    onError: (error: Error) => {
      toast.error(`Rollback failed: ${error.message}`)
    },
  })

  // Mutation for registry reload
  const reloadMutation = useMutation({
    mutationFn: async () => {
      const response = await fetch('/api/v1/admin/profiles/reload', {
        method: 'POST',
        credentials: 'include',
      })
      if (!response.ok) {
        throw new Error('Failed to reload profiles')
      }
      return response.json()
    },
    onSuccess: (data) => {
      toast.success(`Profiles reloaded successfully (${data.profiles_loaded} loaded)`)
      queryClient.invalidateQueries({ queryKey: ['admin', 'profile-updates'] })
    },
    onError: (error: Error) => {
      toast.error(`Reload failed: ${error.message}`)
    },
  })

  const handleRefreshCache = () => {
    refetch()
    toast.info('Refreshing version data...')
  }

  const handleVersionToggle = (profileName: string, version: string, checked: boolean) => {
    setSelectedVersions(prev => {
      const current = prev[profileName] || []
      if (checked) {
        return { ...prev, [profileName]: [...current, version] }
      } else {
        return { ...prev, [profileName]: current.filter(v => v !== version) }
      }
    })
  }

  const handleSelectAll = (profileName: string, versions: string[]) => {
    setSelectedVersions(prev => ({ ...prev, [profileName]: versions }))
  }

  const handleDeselectAll = (profileName: string) => {
    setSelectedVersions(prev => {
      const newState = { ...prev }
      delete newState[profileName]
      return newState
    })
  }

  const handleUpdateProfile = (profile: ProfileVersionStatus) => {
    const selected = selectedVersions[profile.profile_name] || []
    if (selected.length === 0) {
      toast.error('Please select at least one version to add')
      return
    }

    // Determine if this is OpenShift or Kubernetes profile
    const isOpenShift = profile.profile_name.includes('openshift') ||
                        profile.profile_name.includes('rosa') ||
                        profile.profile_name.includes('sno')

    const updateRequest: UpdateVersionsRequest = {
      dry_run: dryRunMode,
    }

    if (isOpenShift) {
      // Merge current versions with selected new versions
      const mergedVersions = [...profile.current_versions, ...selected]
      // Remove duplicates and sort
      const uniqueVersions = Array.from(new Set(mergedVersions)).sort()
      updateRequest.openshift_versions = uniqueVersions
    } else {
      // Kubernetes versions (EKS, GKE)
      const mergedVersions = [...profile.current_versions, ...selected]
      const uniqueVersions = Array.from(new Set(mergedVersions)).sort()
      updateRequest.kubernetes_versions = uniqueVersions
    }

    updateMutation.mutate({
      profileName: profile.profile_name,
      versions: updateRequest,
    })
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-96">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Profile Version Updates</h1>
        <p className="text-muted-foreground">
          Manage cluster profile version allowlists and check for new releases
        </p>
      </div>

      {/* Summary Card */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <div>
            <CardTitle>Update Summary</CardTitle>
            <CardDescription>
              Last checked: {data?.last_checked ? new Date(data.last_checked).toLocaleString() : 'Never'}
            </CardDescription>
          </div>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleRefreshCache}
              disabled={isLoading}
            >
              <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => reloadMutation.mutate()}
              disabled={reloadMutation.isPending}
            >
              <Package className="h-4 w-4 mr-2" />
              Reload Registry
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="flex items-center space-x-4">
              <Package className="h-8 w-8 text-blue-500" />
              <div>
                <p className="text-2xl font-bold">{data?.total_profiles || 0}</p>
                <p className="text-sm text-muted-foreground">Total Profiles</p>
              </div>
            </div>
            <div className="flex items-center space-x-4">
              <ArrowUpCircle className="h-8 w-8 text-orange-500" />
              <div>
                <p className="text-2xl font-bold">{data?.updates_available || 0}</p>
                <p className="text-sm text-muted-foreground">Updates Available</p>
              </div>
            </div>
            <div className="flex items-center space-x-4">
              <Clock className="h-8 w-8 text-green-500" />
              <div>
                <p className="text-2xl font-bold">{data?.cache_age || 'N/A'}</p>
                <p className="text-sm text-muted-foreground">Cache Age</p>
              </div>
            </div>
          </div>

          {/* Dry Run Toggle */}
          <div className="mt-4 flex items-center space-x-2">
            <Checkbox
              id="dry-run"
              checked={dryRunMode}
              onCheckedChange={(checked) => setDryRunMode(checked as boolean)}
            />
            <label
              htmlFor="dry-run"
              className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
            >
              Dry Run Mode (validate without writing)
            </label>
          </div>
        </CardContent>
      </Card>

      {/* No Updates Available */}
      {data?.profiles_with_updates && data.profiles_with_updates.length === 0 && (
        <Alert>
          <Check className="h-4 w-4" />
          <AlertTitle>All profiles are up to date!</AlertTitle>
          <AlertDescription>
            No new versions are available for any of your cluster profiles.
          </AlertDescription>
        </Alert>
      )}

      {/* Profile Update Cards */}
      {data?.profiles_with_updates?.map((profile) => {
        const selected = selectedVersions[profile.profile_name] || []
        const hasSelection = selected.length > 0

        return (
          <Card key={profile.profile_name}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle className="text-xl">{profile.profile_name}</CardTitle>
                  <CardDescription>
                    {profile.update_count} new version{profile.update_count > 1 ? 's' : ''} available
                  </CardDescription>
                </div>
                <Badge variant="secondary">
                  {profile.new_versions.length} updates
                </Badge>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              {/* Current Versions */}
              <div>
                <h4 className="text-sm font-semibold mb-2">Current Versions</h4>
                <div className="flex flex-wrap gap-2">
                  {profile.current_versions.map((version) => (
                    <Badge key={version} variant="outline">
                      {version}
                    </Badge>
                  ))}
                </div>
              </div>

              <Separator />

              {/* New Versions Available */}
              <div>
                <div className="flex items-center justify-between mb-2">
                  <h4 className="text-sm font-semibold">New Versions (Select to Add)</h4>
                  <div className="flex gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleSelectAll(profile.profile_name, profile.new_versions)}
                    >
                      Select All
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleDeselectAll(profile.profile_name)}
                    >
                      Clear
                    </Button>
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  {profile.new_versions.map((version) => {
                    const isSelected = selected.includes(version)
                    return (
                      <div
                        key={version}
                        className={`px-3 py-1 border rounded-md cursor-pointer transition-colors ${
                          isSelected
                            ? 'bg-primary text-primary-foreground border-primary'
                            : 'hover:bg-muted'
                        }`}
                        onClick={() => handleVersionToggle(profile.profile_name, version, !isSelected)}
                      >
                        <div className="flex items-center gap-2">
                          <Checkbox
                            checked={isSelected}
                            onCheckedChange={(checked) =>
                              handleVersionToggle(profile.profile_name, version, checked as boolean)
                            }
                          />
                          <span className="text-sm font-medium">{version}</span>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>

              {/* Selected Versions Summary */}
              {hasSelection && (
                <Alert>
                  <AlertCircle className="h-4 w-4" />
                  <AlertTitle>Selected for addition</AlertTitle>
                  <AlertDescription>
                    {selected.length} version{selected.length > 1 ? 's' : ''} will be added to the allowlist: {selected.join(', ')}
                  </AlertDescription>
                </Alert>
              )}

              {/* Action Buttons */}
              <div className="flex gap-2">
                <Button
                  onClick={() => handleUpdateProfile(profile)}
                  disabled={!hasSelection || updateMutation.isPending}
                  className="flex-1"
                >
                  {updateMutation.isPending ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      {dryRunMode ? 'Validating...' : 'Updating...'}
                    </>
                  ) : (
                    <>
                      <Check className="mr-2 h-4 w-4" />
                      {dryRunMode ? 'Validate Update' : 'Update Profile'}
                    </>
                  )}
                </Button>
                <Button
                  variant="outline"
                  onClick={() => rollbackMutation.mutate(profile.profile_name)}
                  disabled={rollbackMutation.isPending}
                >
                  {rollbackMutation.isPending ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Undo2 className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
