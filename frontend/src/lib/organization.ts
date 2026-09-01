export const SELECTED_ORG_KEY = 'selected_organization_id'

/**
 * Resolve the active organization ID for client-side scoping.
 * Super admins override JWT org via localStorage (mirrors X-Organization-ID on API calls).
 */
export function getActiveOrganizationId(jwtOrgId?: string | null): string {
  return localStorage.getItem(SELECTED_ORG_KEY) || jwtOrgId || ''
}
