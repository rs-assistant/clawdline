/**
 * Whether Settings shows the same-network switch (`cloud_direct_local_network`,
 * `POST /v1/cloud/direct/local-network`).
 *
 * It only changes what a direct connection may use, so it is shown only while
 * direct connections are on, and only by a daemon that reports the field at
 * all: an older one has no route to change it.
 */
export function directLocalNetworkShown(status: {
  enabled?: boolean
  terminal_direct?: boolean
  direct_local_network?: boolean
}): boolean {
  return status.enabled === true && status.terminal_direct === true && typeof status.direct_local_network === "boolean"
}
