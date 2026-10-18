import type { DailyCount, Overview } from "./api.js"

/** A fixed example shape, dated to the viewer's UTC calendar day. Never sent to Cloud. */
export function demoOverview(now = new Date()): Overview {
  const today = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()))
  const day = (ago: number) => new Date(today.getTime() - ago * 86_400_000).toISOString().slice(0, 10)
  const series = (values: number[]): DailyCount[] => values.map((count, i) => ({ day: day(values.length - i), count }))
  const accounts = series([8, 6, 9, 11, 7, 13, 10, 12, 9, 15, 11, 14, 12, 17, 13, 12, 18, 16, 14, 20, 18, 21, 17, 24, 19, 23, 21, 28, 25, 31])
  const redemptions = series([2, 1, 3, 4, 2, 5, 4, 4, 6, 5, 7, 6, 5, 8, 7, 6, 10, 9, 8, 11, 10, 13, 12, 14, 11, 16, 15, 18, 17, 21])
  const sum = (days: DailyCount[]) => days.reduce((total, entry) => total + entry.count, 0)
  const month = (offset: number) => new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth() + offset, 1)).toISOString().slice(0, 7)
  return {
    generated_at: now.toISOString(),
    accounts: {
      total: 4_862,
      new_7d: { current: sum(accounts.slice(-7)), previous: sum(accounts.slice(-14, -7)) },
      new_30d: { current: sum(accounts), previous: 264 },
      daily: accounts,
    },
    promotions: {
      campaigns_total: 3,
      codes_total: 5,
      redemptions_total: 1_274,
      redeemers_total: 1_239,
      redeemed_7d: { current: sum(redemptions.slice(-7)), previous: sum(redemptions.slice(-14, -7)) },
      redeemed_30d: { current: sum(redemptions), previous: 137 },
      daily: redemptions,
      campaigns: {
        total: 3, shown: 3,
        rows: [
          { id: "demo-community", name: "Community Preview", status: "redeemable", not_before: null, expires_at: null, redemption_limit: 5_000, redeemed_count: 891, issued_codes: 2, available_codes: 2 },
          { id: "demo-launch", name: "Launch Invitation", status: "redeemable", not_before: null, expires_at: null, redemption_limit: 1_000, redeemed_count: 286, issued_codes: 2, available_codes: 1 },
          { id: "demo-early", name: "Early Access", status: "campaign_exhausted", not_before: null, expires_at: null, redemption_limit: 97, redeemed_count: 97, issued_codes: 1, available_codes: 0 },
        ],
      },
    },
    usage: {
      current: { period: month(0), coverage: "partial", active_accounts: 1_124, counters: { dispatches: 8_460, connect_minutes: 192_480, bytes_in: 7_840_000_000, bytes_out: 5_120_000_000 } },
      previous: { period: month(-1), coverage: "available", active_accounts: 983, counters: { dispatches: 7_215, connect_minutes: 176_920, bytes_in: 6_930_000_000, bytes_out: 4_680_000_000 } },
    },
  }
}
