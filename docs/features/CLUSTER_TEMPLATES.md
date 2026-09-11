# Cluster Creation Templates

## Overview

Cluster creation templates let a user save the parameters they commonly use when
requesting a cluster and reload them later to pre-populate the create-cluster form.
When a template is applied, every field stored in the template is filled in and any
field not present in the template is left for the user to complete. The **cluster name
is never stored in, or populated from, a template** — it must always be entered per
cluster. **Sensitive data is likewise never stored in a template**: the custom pull
secret (registry credentials) is excluded from capture and stripped server-side, so it
must be re-entered per cluster.

Templates are **private to each user**: a template is only ever visible to, editable
by, and deletable by its owner. Each user may keep at most **5 templates**.

A template can be **saved as new or overwritten** from the create-cluster form, so the
form doubles as an editor for a loaded template. After a cluster is created with
settings that don't match any saved template, the user is **prompted on the new
cluster's detail page** to save those settings as a template.

This feature is unrelated to the pre-existing `PostConfigTemplate` (post-deployment
add-on/operator presets). Post-config templates describe *what to install after* a
cluster is ready; cluster templates describe *how to request* the cluster itself.

## Goals

- Eliminate repetitive re-entry of the same platform / profile / region / team /
  cost-center / TTL / SSH key / tags / add-ons on every cluster request.
- Keep the mechanism decoupled from the ever-growing set of create-cluster fields.
- Guarantee cluster names are always unique and human-chosen (never templated).

## Non-Goals

- Sharing templates between users or teams (explicitly private-only for now).
- Server-side validation of template contents against profiles. A template is a
  convenience preset; the authoritative validation still runs at cluster-creation time.
- Renaming a template. Contents can be overwritten in place, but a template's name is
  fixed once created; to rename, delete and re-save.

## Design Decisions

### 1. Store the form state, not the API request (config → decision → outcome)

- **Context:** The create-cluster HTTP request (`CreateClusterRequest`) and the web
  form (`CreateClusterFormData`) have different shapes. The form has UI-only fields
  (`override_work_hours`, `work_hours_start/end`, `work_days`) that are collapsed into a
  single `work_hours` object only at submit time.
- **Decision:** A template stores the **form field values** (a partial
  `CreateClusterFormData`), serialized as opaque JSON. Applying a template is a direct
  field-by-field restore into the form; saving is a direct capture of current form
  values.
- **Outcome:** The backend never needs to track the create-cluster field set. New form
  fields become templatable simply by adding them to a single `TEMPLATE_FIELDS` list on
  the frontend. Backend and frontend stay decoupled.

### 2. Opaque JSONB config on the backend

- **Context:** Mirroring the typed `PostConfigTemplate.Config` would require a Go struct
  duplicating every form field and would drift as the form evolves.
- **Decision:** `ClusterTemplate.Config` is `json.RawMessage` persisted to a `JSONB`
  column. The backend validates only that the config is a JSON object and strips the keys
  a template must never carry (`name` and the sensitive `custom_pull_secret`).
- **Outcome:** Minimal backend surface; the frontend owns the shape. The strip is a
  defense-in-depth guarantee that the "name is never templated" and "no sensitive data is
  templated" rules hold even if a client sends those keys.

### 2a. Sensitive data is never templated (config → decision → outcome)

- **Context:** The create-cluster form carries a `custom_pull_secret` (additional registry
  credentials). Persisting it in a template would store secret material in the
  `cluster_templates` JSONB column and replay it into the form whenever the template is
  applied.
- **Decision:** `custom_pull_secret` is excluded from `TEMPLATE_FIELDS` on the frontend
  (never captured, never restored) and is stripped server-side in `sanitizeTemplateConfig`
  alongside `name`.
- **Outcome:** Templates hold only non-sensitive convenience parameters; the pull secret is
  re-entered per cluster. Both layers enforce it, so a hand-crafted API request cannot
  smuggle a secret into a template. (An SSH *public* key is not treated as sensitive — it is
  meant to be shared — and remains templatable.)

### 3. Template values win over profile defaults

- **Context:** Selecting a profile triggers an effect that sets default version, region,
  TTL, credentials mode, base domain, and add-ons. If a template set those fields first,
  the profile effect would clobber them.
- **Decision:** Applying a template sets platform/cluster-type state and all fields
  immediately, and records the template in a `pendingTemplate` ref. The profile-defaults
  effect re-applies the pending template *after* it writes profile defaults, then clears
  the ref.
- **Outcome:** A template's explicit region/version/TTL survive even though selecting its
  profile also fires the defaults effect. Fields the template does not specify still fall
  back to profile defaults.

### 4. Per-user limit of 5, enforced atomically (config → decision → outcome)

- **Context:** Unlimited templates clutter the load/overwrite pickers and give unbounded
  per-user storage. The limit must hold even if a client bypasses the UI or double-submits.
- **Decision:** `MaxTemplatesPerUser = 5`. `ClusterTemplateStore.Create` counts the
  owner's rows and inserts **within one transaction**, returning `ErrTemplateLimitReached`
  when at the cap; the handler maps that to a 400. The UI mirrors the constant: at 5, the
  "New template" mode is disabled and only overwrite is offered. Overwrites (Update) never
  count against the limit.
- **Outcome:** The cap is authoritative server-side; the UI degrades gracefully rather
  than letting a save fail opaquely.

### 5. Post-create prompt lives on the cluster detail page (config → decision → outcome)

- **Context:** After a successful create the form navigates to `/clusters/:id`, and the
  App Router cannot carry in-memory state across that navigation. The prompt should only
  appear when the just-used settings aren't already saved.
- **Decision:** On successful create, the form stashes its `getCurrentConfig()` snapshot in
  `sessionStorage` under `ocpctl:new-cluster-template-config`, then navigates. The detail
  page mounts `SaveClusterSettingsPrompt`, which consumes (and clears) that key once,
  compares the config against every saved template using a canonical (sorted-key,
  undefined-dropped) JSON encoding, and prompts only when there is no match.
- **Outcome:** No cross-page state coupling; the prompt is best-effort (a missing/blocked
  `sessionStorage` simply means no prompt) and never nags a user whose settings are
  already templated.

## Data Model

New table (migration `00068_cluster_templates.sql`):

```sql
CREATE TABLE cluster_templates (
  id VARCHAR(64) PRIMARY KEY DEFAULT gen_random_uuid()::text,
  name VARCHAR(255) NOT NULL,
  owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  config JSONB NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  UNIQUE (owner_id, name)
);
CREATE INDEX idx_cluster_templates_owner ON cluster_templates(owner_id);
```

- `UNIQUE (owner_id, name)` — a user cannot have two templates with the same name; a
  save with a duplicate name fails and surfaces as an error in the dialog.
- `ON DELETE CASCADE` — templates are removed when the owning user is deleted.

Go type (`pkg/types/cluster_template.go`):

```go
type ClusterTemplate struct {
    ID        string          `db:"id" json:"id"`
    Name      string          `db:"name" json:"name"`
    OwnerID   string          `db:"owner_id" json:"ownerId"`
    Config    json.RawMessage `db:"config" json:"config"`
    CreatedAt time.Time       `db:"created_at" json:"createdAt"`
    UpdatedAt time.Time       `db:"updated_at" json:"updatedAt"`
}
```

## What a Template Stores

All create-cluster form fields **except `name` and `custom_pull_secret`**. `owner` and the
transient `idempotency_key` are not meaningful reusable parameters and are excluded from the
capture list (`owner` is always the requesting user; `idempotency_key` is a per-request
dedup token). `custom_pull_secret` is excluded because it is sensitive (see Design
Decision 2a).

Captured fields (`TEMPLATE_FIELDS`):

```
platform, cluster_type, version, profile, region, base_domain,
owner, team, cost_center, ttl_hours, ssh_public_key, extra_tags,
offhours_opt_in, skip_post_deployment, postConfigAddOns, customPostConfig,
enable_efs_storage, preserve_on_failure, credentials_mode,
override_work_hours, work_hours_enabled, work_hours_start, work_hours_end, work_days
```

> Note: `owner` is listed above because the form carries it, but the backend strips
> `name` and `custom_pull_secret`; `owner` is harmless (always the same user for a private
> template). The hard rules are: **`name` is never stored and never applied**, and **no
> sensitive data (`custom_pull_secret`) is ever stored or applied.**

## API

All routes require authentication and are scoped to the authenticated user
(`/api/v1/cluster-templates`):

| Method | Path                        | Description                          |
|--------|-----------------------------|--------------------------------------|
| POST   | `/cluster-templates`        | Create a template                    |
| GET    | `/cluster-templates`        | List the caller's templates          |
| GET    | `/cluster-templates/:id`    | Get one of the caller's templates    |
| PATCH  | `/cluster-templates/:id`    | Update the caller's template         |
| DELETE | `/cluster-templates/:id`    | Delete the caller's template         |

Request body (create/update):

```json
{
  "name": "My standard AWS SNO",
  "config": {
    "platform": "aws",
    "cluster_type": "openshift",
    "profile": "aws-sno-ga",
    "region": "us-east-1",
    "team": "platform",
    "cost_center": "733",
    "ttl_hours": 72
  }
}
```

The server strips the `name` and `custom_pull_secret` keys inside `config` before
persisting. `POST` fails with a 400 when the caller already has `MaxTemplatesPerUser` (5)
templates; overwriting via `PATCH` is unaffected by the limit.

## Backend Components

- `pkg/types/cluster_template.go` — `ClusterTemplate` type.
- `internal/store/migrations/00068_cluster_templates.sql` — schema.
- `internal/store/cluster_templates.go` — `ClusterTemplateStore` with owner-scoped
  `Create` / `GetByID` / `List` / `Update` / `Delete`.
- `internal/store/store.go` — `Store.ClusterTemplates` wiring.
- `internal/api/handler_cluster_templates.go` — `ClusterTemplateHandler` (bind →
  validate → `sanitizeTemplateConfig` → store). `sanitizeTemplateConfig` enforces the
  JSON-object shape and strips the `templateStrippedKeys` (`name`, `custom_pull_secret`).
- `internal/api/server.go` — route registration.

Ownership is enforced at the SQL layer: every query filters by `owner_id`, so there is
no cross-user read/update/delete path even with a guessed ID.

## Frontend Components

- `web/types/api.ts` — `ClusterTemplate`, `ClusterTemplatesResponse`.
- `web/lib/api/endpoints/clusterTemplates.ts` — API client (`list/get/create/update/delete`).
- `web/lib/api/index.ts` — export.
- `web/lib/hooks/useClusterTemplates.ts` — React Query hooks (`useClusterTemplates`,
  `useCreateClusterTemplate`, `useUpdateClusterTemplate`, `useDeleteClusterTemplate`).
- `web/components/clusters/SaveTemplateDialog.tsx` — shared save dialog with New/Overwrite
  modes; exports `MAX_CLUSTER_TEMPLATES` (mirrors the backend constant). Used by both the
  create-page bar and the post-create prompt.
- `web/components/clusters/ClusterTemplateBar.tsx` — the load/apply/delete control bar plus
  the "Save as template" button that opens `SaveTemplateDialog`.
- `web/components/clusters/SaveClusterSettingsPrompt.tsx` — consumes the stashed config on
  the cluster detail page and, if unmatched, prompts to save it (via `SaveTemplateDialog`).
- `web/app/(dashboard)/clusters/new/page.tsx` — integration:
  - `ClusterTemplateBar` rendered above the form.
  - `getCurrentConfig()` snapshots form values (minus `name`) for save.
  - `handleApplyTemplate()` sets platform/cluster-type state, applies fields, and queues
    a re-apply via `pendingTemplate`.
  - The profile-defaults effect re-applies `pendingTemplate` after writing defaults.
  - On successful create, stashes `getCurrentConfig()` in `sessionStorage` before navigating.
- `web/app/(dashboard)/clusters/[id]/page.tsx` — renders `SaveClusterSettingsPrompt`.

## User Flow

1. On **Create Cluster**, the user selects a saved template and clicks **Apply**. The
   form fills with the template's values; the cluster name field stays empty.
2. The user enters a unique cluster name and adjusts any remaining fields.
3. Alternatively, after filling the form the user clicks **Save as template** and either
   names a **new** template or picks an existing one to **overwrite** (editing a loaded
   template in place).
4. A template can be removed with the delete (trash) button next to the selector.
5. After creating a cluster, if the settings used don't match any saved template, the new
   cluster's detail page prompts **"Save these settings as a template?"** — the user can
   dismiss it or save (as new, or overwrite if at the 5-template limit).

## Edge Cases

- **Template profile no longer valid** (disabled/removed, or wrong track filter): the
  form's existing "clear profile if invalid" effect clears the profile; other
  non-profile-dependent template fields still apply. The user re-selects a profile.
- **Duplicate template name:** blocked by the `UNIQUE (owner_id, name)` constraint;
  surfaced as an error in the save dialog.
- **Config with a stray `name`:** stripped server-side; never applied client-side
  (`name` is not in `TEMPLATE_FIELDS`).
- **Config with a stray `custom_pull_secret`:** stripped server-side; never captured or
  applied client-side (`custom_pull_secret` is not in `TEMPLATE_FIELDS`). Applying a
  template leaves the pull-secret field empty for the user to fill in.
- **Platform/cluster-type in template:** applied to both the react-hook-form values and
  the separate `selectedPlatform` / `selectedClusterType` component state that drives
  profile filtering.
- **At the 5-template limit:** the save dialog disables "New template" and offers only
  overwrite; a `POST` that slips through is rejected 400 by the backend.
- **Post-create prompt with no match:** shown once per created cluster; dismissing it or
  reloading the detail page (the `sessionStorage` key is cleared on first read) does not
  re-prompt. If the settings already match a saved template, no prompt appears.

## Testing

- Backend: unit tests for `sanitizeTemplateConfig` (object required, `name` and
  `custom_pull_secret` stripped) and owner-scoping of `ClusterTemplateStore` methods.
- Frontend: apply-template pre-fills fields and leaves `name` empty; template values
  survive the profile-defaults effect; save captures current values minus `name`.

## Future Enhancements

- Rename existing templates (overwrite of contents is already supported).
- Optional team-shared or public templates (would reintroduce an `is_public`/visibility
  model like `PostConfigTemplate`).
- A dedicated template management page for bulk view/edit/delete.
