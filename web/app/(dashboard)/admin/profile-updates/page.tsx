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
import { useAuthStore } from '@/lib/stores/authStore'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  RefreshCw,
  Check,
  X,
  AlertCircle,
  Loader2,
  Package,
  ArrowUpCircle,
  Clock,
  Undo2,
  Search,
  Filter,
  Star,
  XCircle
} from 'lucide-react'

// Semantic version comparison helper
function compareVersions(a: string, b: string): number {
  const aParts = a.split('.').map(Number)
  const bParts = b.split('.').map(Number)

  for (let i = 0; i < Math.max(aParts.length, bParts.length); i++) {
    const aNum = aParts[i] || 0
    const bNum = bParts[i] || 0

    if (aNum !== bNum) {
      return aNum - bNum
    }
  }

  return 0
}

interface ProfileVersionStatus {
  profile_name: string
  current_versions: string[]
  default_version: string
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
  openshift_default_version?: string
  kubernetes_default_version?: string
  dry_run?: boolean
}

export default function ProfileUpdatesPage() {
  const queryClient = useQueryClient()
  const [selectedVersions, setSelectedVersions] = useState<Record<string, string[]>>({})
  const [removedVersions, setRemovedVersions] = useState<Record<string, string[]>>({})
  const [newDefaults, setNewDefaults] = useState<Record<string, string>>({})
  const [dryRunMode, setDryRunMode] = useState(false)
  const [searchFilter, setSearchFilter] = useState('')
  const [clusterTypeFilter, setClusterTypeFilter] = useState<string>('all')
  const [showOnlyWithUpdates, setShowOnlyWithUpdates] = useState(true)

  // Fetch version check data
  const { data, isLoading, refetch } = useQuery<CheckVersionsResponse>({
    queryKey: ['admin', 'profile-updates'],
    queryFn: async () => {
      const token = useAuthStore.getState().accessToken
      if (!token) {
        throw new Error('Not authenticated')
      }
      const response = await fetch('/api/v1/admin/profiles/version-check', {
        headers: {
          'Authorization': `Bearer ${token}`,
        },
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
      const token = useAuthStore.getState().accessToken
      if (!token) {
        throw new Error('Not authenticated')
      }
      const response = await fetch(`/api/v1/admin/profiles/${profileName}/update-versions`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`,
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
        // Clear all changes for this profile
        setSelectedVersions(prev => {
          const newState = { ...prev }
          delete newState[variables.profileName]
          return newState
        })
        setRemovedVersions(prev => {
          const newState = { ...prev }
          delete newState[variables.profileName]
          return newState
        })
        setNewDefaults(prev => {
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
      const token = useAuthStore.getState().accessToken
      if (!token) {
        throw new Error('Not authenticated')
      }
      const response = await fetch(`/api/v1/admin/profiles/${profileName}/rollback`, {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`,
        },
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
      const token = useAuthStore.getState().accessToken
      if (!token) {
        throw new Error('Not authenticated')
      }
      const response = await fetch('/api/v1/admin/profiles/reload', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`,
        },
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

  const handleRemoveVersion = (profileName: string, version: string) => {
    setRemovedVersions(prev => {
      const current = prev[profileName] || []
      if (current.includes(version)) {
        return prev
      }
      return { ...prev, [profileName]: [...current, version] }
    })
  }

  const handleUndoRemove = (profileName: string, version: string) => {
    setRemovedVersions(prev => {
      const current = prev[profileName] || []
      return { ...prev, [profileName]: current.filter(v => v !== version) }
    })
  }

  const handleSetDefault = (profileName: string, version: string) => {
    setNewDefaults(prev => ({ ...prev, [profileName]: version }))
  }

  const handleUpdateProfile = (profile: ProfileVersionStatus) => {
    const selected = selectedVersions[profile.profile_name] || []
    const removed = removedVersions[profile.profile_name] || []
    const newDefault = newDefaults[profile.profile_name]

    // Check if any changes were made
    if (selected.length === 0 && removed.length === 0 && !newDefault) {
      toast.error('Please make at least one change (add, remove, or change default)')
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
      // Start with current versions, remove the ones marked for removal, add selected
      let versions = profile.current_versions.filter(v => !removed.includes(v))
      versions = [...versions, ...selected]
      // Remove duplicates and sort
      const uniqueVersions = Array.from(new Set(versions)).sort(compareVersions)

      // Validate we're not removing all versions
      if (uniqueVersions.length === 0) {
        toast.error('Cannot remove all versions from profile')
        return
      }

      updateRequest.openshift_versions = uniqueVersions

      // Set default version if changed
      if (newDefault) {
        // Validate default is in the final list
        if (!uniqueVersions.includes(newDefault)) {
          toast.error('Cannot set default to a version that is being removed')
          return
        }
        updateRequest.openshift_default_version = newDefault
      }
    } else {
      // Kubernetes versions (EKS, GKE)
      let versions = profile.current_versions.filter(v => !removed.includes(v))
      versions = [...versions, ...selected]
      const uniqueVersions = Array.from(new Set(versions)).sort(compareVersions)

      if (uniqueVersions.length === 0) {
        toast.error('Cannot remove all versions from profile')
        return
      }

      updateRequest.kubernetes_versions = uniqueVersions

      if (newDefault) {
        if (!uniqueVersions.includes(newDefault)) {
          toast.error('Cannot set default to a version that is being removed')
          return
        }
        updateRequest.kubernetes_default_version = newDefault
      }
    }

    updateMutation.mutate({
      profileName: profile.profile_name,
      versions: updateRequest,
    })
  }

  // Filter profiles based on search and filters
  const filteredProfiles = (data?.profiles_with_updates || []).filter(profile => {
    // Search filter
    if (searchFilter && !profile.profile_name.toLowerCase().includes(searchFilter.toLowerCase())) {
      return false
    }

    // Cluster type filter
    if (clusterTypeFilter !== 'all') {
      const profileName = profile.profile_name.toLowerCase()
      if (clusterTypeFilter === 'openshift' && !profileName.includes('openshift') && !profileName.includes('rosa') && !profileName.includes('sno') && !profileName.includes('gcp-standard')) {
        return false
      }
      if (clusterTypeFilter === 'eks' && !profileName.includes('eks')) {
        return false
      }
      if (clusterTypeFilter === 'gke' && !profileName.includes('gke')) {
        return false
      }
      if (clusterTypeFilter === 'iks' && !profileName.includes('iks')) {
        return false
      }
    }

    // Show only with updates filter
    if (showOnlyWithUpdates && profile.update_count === 0) {
      return false
    }

    return true
  })

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

      {/* Filter Controls */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg flex items-center gap-2">
            <Filter className="h-5 w-5" />
            Filter Profiles
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {/* Search Filter */}
            <div className="space-y-2">
              <Label htmlFor="search">Search by Name</Label>
              <div className="relative">
                <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  id="search"
                  placeholder="Filter profiles..."
                  value={searchFilter}
                  onChange={(e) => setSearchFilter(e.target.value)}
                  className="pl-8"
                />
              </div>
            </div>

            {/* Cluster Type Filter */}
            <div className="space-y-2">
              <Label htmlFor="cluster-type">Cluster Type</Label>
              <Select value={clusterTypeFilter} onValueChange={setClusterTypeFilter}>
                <SelectTrigger id="cluster-type">
                  <SelectValue placeholder="All cluster types" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All Types</SelectItem>
                  <SelectItem value="openshift">OpenShift</SelectItem>
                  <SelectItem value="eks">EKS</SelectItem>
                  <SelectItem value="gke">GKE</SelectItem>
                  <SelectItem value="iks">IKS</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* Show Only With Updates */}
            <div className="space-y-2">
              <Label htmlFor="updates-only">Display Options</Label>
              <div className="flex items-center space-x-2 h-10">
                <Checkbox
                  id="updates-only"
                  checked={showOnlyWithUpdates}
                  onCheckedChange={(checked) => setShowOnlyWithUpdates(checked as boolean)}
                />
                <label
                  htmlFor="updates-only"
                  className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
                >
                  Show only profiles with updates
                </label>
              </div>
            </div>
          </div>

          {/* Filter Results Summary */}
          {data?.profiles_with_updates && (
            <div className="mt-4 text-sm text-muted-foreground">
              Showing {filteredProfiles.length} of {data.profiles_with_updates.length} profile{data.profiles_with_updates.length !== 1 ? 's' : ''}
              {showOnlyWithUpdates && data.updates_available > 0 && (
                <span> • {data.updates_available} with updates</span>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* No Results */}
      {filteredProfiles.length === 0 && !isLoading && (
        <Alert>
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>No profiles match your filters</AlertTitle>
          <AlertDescription>
            {searchFilter || clusterTypeFilter !== 'all'
              ? 'Try adjusting your search or filter criteria.'
              : 'No new versions are available for any of your cluster profiles.'}
          </AlertDescription>
        </Alert>
      )}

      {/* Profile Update Cards */}
      {filteredProfiles.map((profile) => {
        const selected = selectedVersions[profile.profile_name] || []
        const removed = removedVersions[profile.profile_name] || []
        const newDefault = newDefaults[profile.profile_name]
        const currentDefault = newDefault || profile.default_version
        const hasChanges = selected.length > 0 || removed.length > 0 || newDefault !== undefined

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
                <div className="flex items-center justify-between mb-2">
                  <h4 className="text-sm font-semibold">Current Versions</h4>
                  <p className="text-xs text-muted-foreground">
                    Click star to set default • Click X to remove
                  </p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {[...profile.current_versions].sort(compareVersions).map((version) => {
                    const isRemoved = removed.includes(version)
                    const isDefault = version === currentDefault
                    const isNewDefault = version === newDefault

                    return (
                      <div
                        key={version}
                        className={`group relative px-3 py-1 border rounded-md transition-all ${
                          isRemoved
                            ? 'bg-destructive/10 border-destructive/50 line-through opacity-50'
                            : isNewDefault
                            ? 'bg-yellow-500/20 border-yellow-500'
                            : isDefault
                            ? 'bg-primary/10 border-primary'
                            : 'hover:bg-muted'
                        }`}
                      >
                        <div className="flex items-center gap-1.5">
                          <button
                            onClick={() =>
                              isRemoved
                                ? handleUndoRemove(profile.profile_name, version)
                                : handleSetDefault(profile.profile_name, version)
                            }
                            className="flex items-center"
                            title={isRemoved ? 'Undo remove' : isDefault ? 'Current default' : 'Set as default'}
                          >
                            <Star
                              className={`h-3.5 w-3.5 ${
                                isDefault || isNewDefault
                                  ? 'fill-yellow-500 text-yellow-500'
                                  : 'text-muted-foreground hover:text-yellow-500'
                              }`}
                            />
                          </button>
                          <span className="text-sm font-medium">{version}</span>
                          {!isRemoved ? (
                            <button
                              onClick={() => handleRemoveVersion(profile.profile_name, version)}
                              className="opacity-0 group-hover:opacity-100 transition-opacity"
                              title="Remove version"
                            >
                              <XCircle className="h-3.5 w-3.5 text-destructive hover:text-destructive/80" />
                            </button>
                          ) : (
                            <button
                              onClick={() => handleUndoRemove(profile.profile_name, version)}
                              className="opacity-100"
                              title="Undo remove"
                            >
                              <Undo2 className="h-3.5 w-3.5 text-muted-foreground hover:text-foreground" />
                            </button>
                          )}
                        </div>
                      </div>
                    )
                  })}
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
                  {[...profile.new_versions].sort(compareVersions).map((version) => {
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
                        <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                          <Checkbox
                            checked={isSelected}
                            onCheckedChange={(checked) =>
                              handleVersionToggle(profile.profile_name, version, checked as boolean)
                            }
                          />
                          <span className="text-sm font-medium" onClick={(e) => {
                            e.stopPropagation()
                            handleVersionToggle(profile.profile_name, version, !isSelected)
                          }}>{version}</span>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>

              {/* Changes Summary */}
              {hasChanges && (
                <Alert>
                  <AlertCircle className="h-4 w-4" />
                  <AlertTitle>Pending Changes</AlertTitle>
                  <AlertDescription className="space-y-1">
                    {selected.length > 0 && (
                      <div>
                        <span className="font-semibold">Add:</span> {selected.join(', ')}
                      </div>
                    )}
                    {removed.length > 0 && (
                      <div>
                        <span className="font-semibold text-destructive">Remove:</span> {removed.join(', ')}
                      </div>
                    )}
                    {newDefault && (
                      <div>
                        <span className="font-semibold text-yellow-600">New Default:</span> {newDefault}
                        {profile.default_version && profile.default_version !== newDefault && (
                          <span className="text-muted-foreground"> (was {profile.default_version})</span>
                        )}
                      </div>
                    )}
                  </AlertDescription>
                </Alert>
              )}

              {/* Action Buttons */}
              <div className="flex gap-2">
                <Button
                  onClick={() => handleUpdateProfile(profile)}
                  disabled={!hasChanges || updateMutation.isPending}
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
