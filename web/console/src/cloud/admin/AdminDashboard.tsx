import { useId } from "react"
import { campaignFill, trend, type CampaignRow, type Comparison, type DailyCount, type Overview, type UsagePeriod } from "./api.js"
import type { Say } from "./words.js"

/*
 * The overview, drawn. Three sections in the order an operator asks: are
 * people arriving, are the campaigns working, what is the service carrying.
 * Every figure the API could not vouch for is shown as the word for unknown,
 * every chart has its numbers in words beside it and a table behind it, and
 * no change is told by colour alone.
 */

export function AdminDashboard({ overview, say, locale, stale }: {
  overview: Overview
  say: Say
  locale: string
  stale: { code: string } | null
}) {
  const n = new Intl.NumberFormat(locale)
  const { accounts, promotions, usage } = overview
  return (
    <div className="adm-dash">
      {stale ? (
        <p className="adm-stale" role="status">
          {say("staleNote", { code: stale.code, time: new Intl.DateTimeFormat(locale, { timeZone: "UTC", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(new Date(overview.generated_at)) })}
        </p>
      ) : null}

      <section className="adm-section" aria-labelledby="adm-accounts">
        <h2 id="adm-accounts" className="adm-h2">{say("sectionAccounts")}</h2>
        <div className="adm-tiles">
          <Tile label={say("accountsTotal")} value={n.format(accounts.total)} lead />
          <TrendTile label={say("accountsNew7")} value={accounts.new_7d} say={say} n={n} />
          <TrendTile label={say("accountsNew30")} value={accounts.new_30d} say={say} n={n} />
        </div>
        <p className="adm-fine">{say("comparisonNote")}</p>
        <DailyChart title={say("chartAccounts")} days={accounts.daily} say={say} locale={locale} />
      </section>

      <section className="adm-section" aria-labelledby="adm-promotions">
        <h2 id="adm-promotions" className="adm-h2">{say("sectionPromotions")}</h2>
        <div className="adm-tiles adm-tiles-4">
          <Tile label={say("redemptionsTotal")} value={n.format(promotions.redemptions_total)} lead />
          <Tile label={say("redeemersTotal")} value={n.format(promotions.redeemers_total)} />
          <TrendTile label={say("redeemed7")} value={promotions.redeemed_7d} say={say} n={n} />
          <TrendTile label={say("redeemed30")} value={promotions.redeemed_30d} say={say} n={n} />
        </div>
        <p className="adm-fine">{say("comparisonNote")}</p>
        <p className="adm-fine">
          {say("promoInventory", { campaigns: n.format(promotions.campaigns_total), codes: n.format(promotions.codes_total) })}
        </p>
        <DailyChart title={say("chartRedemptions")} days={promotions.daily} say={say} locale={locale} />
        <Campaigns rows={promotions.campaigns.rows} total={promotions.campaigns.total} say={say} n={n} />
      </section>

      <section className="adm-section" aria-labelledby="adm-usage">
        <h2 id="adm-usage" className="adm-h2">{say("sectionUsage")}</h2>
        <div className="adm-usage">
          <UsageCard heading={say("usageCurrent")} period={usage.current} say={say} locale={locale} n={n} current asOf={overview.generated_at} />
          <UsageCard heading={say("usagePrevious")} period={usage.previous} say={say} locale={locale} n={n} asOf={overview.generated_at} />
        </div>
        <p className="adm-fine">{say("usageNoGrowth")}</p>
      </section>

      <p className="adm-fine adm-foot">{say("windowNote")}</p>
    </div>
  )
}

function Tile({ label, value, lead, children }: { label: string; value: string; lead?: boolean; children?: React.ReactNode }) {
  return (
    <div className={lead ? "adm-tile adm-tile-lead" : "adm-tile"}>
      <p className="adm-tile-label">{label}</p>
      <p className="adm-tile-value">{value}</p>
      {children}
    </div>
  )
}

function TrendTile({ label, value, say, n }: { label: string; value: Comparison; say: Say; n: Intl.NumberFormat }) {
  const t = trend(value)
  const previous = n.format(value.previous)
  let chip: string
  let spoken: string
  if (t.kind === "flat") {
    chip = say("trendFlat")
    spoken = say("trendLabelFlat", { previous })
  } else {
    const delta = n.format(t.delta)
    chip = say(t.kind === "up" ? "trendUp" : "trendDown", { delta })
    if (t.percent !== null) chip += " · " + say("trendPercent", { percent: n.format(t.percent) })
    const key = t.kind === "up"
      ? (t.percent !== null ? "trendLabelUpPercent" : "trendLabelUp")
      : (t.percent !== null ? "trendLabelDownPercent" : "trendLabelDown")
    spoken = say(key, { delta, previous, percent: t.percent ?? 0 })
  }
  return (
    <Tile label={label} value={n.format(value.current)}>
      <p className="adm-trend">
        <span className={"adm-chip adm-chip-" + t.kind} aria-hidden="true">{chip}</span>
        <span className="adm-trend-prev" aria-hidden="true">{say("trendAgainst", { previous })}</span>
        <span className="adm-visually-hidden">{spoken}</span>
      </p>
    </Tile>
  )
}

function shortDay(day: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone: "UTC", month: "numeric", day: "numeric" }).format(new Date(day + "T00:00:00Z"))
}

function DailyChart({ title, days, say, locale }: { title: string; days: DailyCount[]; say: Say; locale: string }) {
  const id = useId()
  const n = new Intl.NumberFormat(locale)
  if (days.length === 0) {
    return (
      <figure className="adm-panel adm-chart">
        <figcaption className="adm-chart-title">{title}</figcaption>
        <p className="adm-fine">{say("chartEmpty")}</p>
      </figure>
    )
  }
  const total = days.reduce((s, d) => s + d.count, 0)
  const peak = days.reduce((best, d) => (d.count > best.count ? d : best), days[0])
  const max = Math.max(1, peak.count)
  const from = shortDay(days[0].day, locale)
  const to = shortDay(days[days.length - 1].day, locale)
  const summary = say("chartSummary", { total: n.format(total), days: days.length, peakDay: shortDay(peak.day, locale), peak: n.format(peak.count) })
  // A fixed drawing box scaled to the panel's width; the bars keep a gap of a
  // fifth of their slot so 30 of them stay distinct at 375 px.
  const W = 300
  const H = 96
  const slot = W / days.length
  const bar = slot * 0.72
  return (
    <figure className="adm-panel adm-chart" aria-labelledby={id + "t"} aria-describedby={id + "s"}>
      <figcaption className="adm-chart-head">
        <span id={id + "t"} className="adm-chart-title">{title}</span>
        <span className="adm-chart-range">{say("chartRange", { from, to })}</span>
      </figcaption>
      <svg className="adm-svg" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={`${title}. ${summary}`}>
        <line x1="0" x2={W} y1={H - 0.5} y2={H - 0.5} className="adm-axis" />
        {days.map((d, i) => {
          const h = d.count === 0 ? 0 : Math.max(2, (d.count / max) * (H - 6))
          return (
            <rect key={d.day} x={i * slot + (slot - bar) / 2} y={H - 1 - h} width={bar} height={h} rx="1"
              className={d === peak && d.count > 0 ? "adm-bar adm-bar-peak" : "adm-bar"}>
              <title>{`${shortDay(d.day, locale)}: ${n.format(d.count)}`}</title>
            </rect>
          )
        })}
      </svg>
      <div className="adm-chart-scale" aria-hidden="true">
        <span>{from}</span>
        <span>{to}</span>
      </div>
      <p id={id + "s"} className="adm-chart-summary">{summary}</p>
      <details className="adm-details">
        <summary>{say("chartTable")}</summary>
        <div className="adm-table-wrap">
          <table className="adm-table adm-table-days">
            <thead><tr><th scope="col">{say("chartDay")}</th><th scope="col" className="num">{say("chartCount")}</th></tr></thead>
            <tbody>
              {[...days].reverse().map((d) => (
                <tr key={d.day}><td>{d.day}</td><td className="num">{n.format(d.count)}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </figure>
  )
}

function campaignWindow(row: CampaignRow, say: Say): string {
  const utc = (value: string) => new Date(value).toISOString().replace("T", " ").replace(/\.000Z$/, "Z")
  const from = row.not_before ? utc(row.not_before) : null
  const to = row.expires_at ? utc(row.expires_at) : null
  if (from && to) return say("windowRange", { from, to })
  if (from) return say("windowFrom", { date: from })
  if (to) return say("windowUntil", { date: to })
  return say("windowAlways")
}

const STATUS_TONE: Record<CampaignRow["status"], string> = {
  redeemable: "ok",
  not_started: "calm",
  expired: "dim",
  revoked: "bad",
  campaign_exhausted: "warn",
  no_available_code: "warn",
  unknown: "dim",
}

function Campaigns({ rows, total, say, n }: { rows: CampaignRow[]; total: number; say: Say; n: Intl.NumberFormat }) {
  return (
    <div className="adm-panel adm-campaigns">
      <h3 className="adm-h3">{say("campaignsTitle")}</h3>
      <p className="adm-fine">{say("campaignEligibilityNote")}</p>
      {total > rows.length ? (
        <p className="adm-fine adm-capped">{say("campaignsCapped", { shown: n.format(rows.length), total: n.format(total) })}</p>
      ) : null}
      {rows.length === 0 ? (
        <p className="adm-fine">{say("campaignsNone")}</p>
      ) : (
        <div className="adm-table-wrap">
          <table className="adm-table adm-table-campaigns">
            <thead>
              <tr>
                <th scope="col">{say("campaignName")}</th>
                <th scope="col">{say("campaignStatus")}</th>
                <th scope="col">{say("campaignWindow")}</th>
                <th scope="col">{say("campaignRedeemed")}</th>
                <th scope="col" className="num">{say("campaignCodes")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const fill = campaignFill(row)
                return (
                  <tr key={row.id}>
                    <th scope="row" data-label={say("campaignName")}>
                      <span className={row.name ? "adm-cname" : "adm-cname adm-unnamed"}>{row.name || say("campaignUnnamed")}</span>
                    </th>
                    <td data-label={say("campaignStatus")}>
                      <span className={"adm-status adm-status-" + STATUS_TONE[row.status]}>
                        <span className="adm-dot" aria-hidden="true" />{say(`status_${row.status}`)}
                      </span>
                    </td>
                    <td data-label={say("campaignWindow")} className="adm-when">{campaignWindow(row, say)}</td>
                    <td data-label={say("campaignRedeemed")}>
                      <span className="adm-fillnum">
                        {row.redemption_limit === null
                          ? say("campaignNoLimit", { n: n.format(row.redeemed_count) })
                          : say("campaignOf", { n: n.format(row.redeemed_count), limit: n.format(row.redemption_limit) })}
                      </span>
                      {fill !== null ? (
                        <span className="adm-meter" aria-hidden="true"><span style={{ width: `${Math.round(fill * 100)}%` }} /></span>
                      ) : null}
                    </td>
                    <td data-label={say("campaignCodes")} className="num">
                      {say("campaignCodesValue", { available: n.format(row.available_codes), issued: n.format(row.issued_codes) })}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function bytes(value: number, locale: string): string {
  const units = ["byte", "kilobyte", "megabyte", "gigabyte", "terabyte"] as const
  let v = value
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return new Intl.NumberFormat(locale, { style: "unit", unit: units[i], unitDisplay: "short", maximumFractionDigits: v < 10 && i > 0 ? 1 : 0 }).format(v)
}

function monthName(period: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone: "UTC", year: "numeric", month: "long" }).format(new Date(period + "-01T00:00:00Z"))
}

function UsageCard({ heading, period, say, locale, n, current = false, asOf }: { heading: string; period: UsagePeriod; say: Say; locale: string; n: Intl.NumberFormat; current?: boolean; asOf: string }) {
  const rows: [string, number | null, (v: number) => string][] = [
    [say("usageActive"), period.active_accounts, (v) => n.format(v)],
    [say("usageDispatches"), period.counters.dispatches, (v) => n.format(v)],
    [say("usageMinutes"), period.counters.connect_minutes, (v) => n.format(v)],
    [say("usageIn"), period.counters.bytes_in, (v) => bytes(v, locale)],
    [say("usageOut"), period.counters.bytes_out, (v) => bytes(v, locale)],
  ]
  return (
    <div className="adm-panel adm-usage-card">
      <div className="adm-usage-head">
        <h3 className="adm-h3">{heading}<span className="adm-month">{monthName(period.period, locale)}</span></h3>
        {period.coverage === "partial" ? <span className="adm-status adm-status-warn"><span className="adm-dot" aria-hidden="true" />{say("usagePartial")}</span> : null}
      </div>
      <p className="adm-fine">{current ? say("usageInProgress", { time: new Date(asOf).toISOString().replace("T", " ").replace(/\.000Z$/, "Z") }) : say("usageEnded")}</p>
      {period.coverage === "partial" ? <p className="adm-fine">{say("usagePartialNote")}</p> : null}
      <dl className="adm-dl">
        {rows.map(([label, value, format]) => (
          <div key={label} className="adm-dl-row">
            <dt>{label}</dt>
            <dd>
              {value === null
                ? <span className="adm-unknown" title={say("usageUnknownLabel")}><span aria-hidden="true">— </span>{say("usageUnknown")}</span>
                : format(value)}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  )
}
