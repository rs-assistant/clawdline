import type { Overview } from "./api.js"
import { DailyChart, Tile, TrendTile } from "./AdminDashboard.js"
import type { Say } from "./words.js"

/** Read-only preview: no campaign or code API is mounted while it is visible. */
export function DemoPromotions({ overview, say, locale }: { overview: Overview; say: Say; locale: string }) {
  const n = new Intl.NumberFormat(locale)
  const { promotions } = overview
  return (
    <div className="adm-page">
      <div className="adm-tiles adm-tiles-4">
        <Tile label={say("redemptionsTotal")} value={n.format(promotions.redemptions_total)} lead />
        <Tile label={say("redeemersTotal")} value={n.format(promotions.redeemers_total)} />
        <TrendTile label={say("redeemed7")} value={promotions.redeemed_7d} say={say} n={n} />
        <TrendTile label={say("redeemed30")} value={promotions.redeemed_30d} say={say} n={n} />
      </div>
      <p className="adm-fine">{say("promoInventory", { campaigns: n.format(promotions.campaigns_total), codes: n.format(promotions.codes_total) })}</p>
      <DailyChart title={say("chartRedemptions")} days={promotions.daily} say={say} locale={locale} />
      <section className="adm-panel adm-campaigns" aria-labelledby="adm-demo-campaigns">
        <div className="adm-panel-head">
          <div>
            <h2 id="adm-demo-campaigns" className="adm-h3">{say("campaignsTitle")}</h2>
            <p className="adm-fine">{say("demoCampaignNote")}</p>
          </div>
        </div>
        <div className="adm-table-wrap">
          <table className="adm-table adm-table-campaigns">
            <thead><tr><th scope="col">{say("campaignName")}</th><th scope="col">{say("campaignStatus")}</th><th scope="col">{say("campaignRedeemed")}</th><th scope="col">{say("campaignCodes")}</th></tr></thead>
            <tbody>{promotions.campaigns.rows.map((row) => (
              <tr key={row.id}>
                <th scope="row" data-label={say("campaignName")}>{row.name}</th>
                <td data-label={say("campaignStatus")}>{say(row.status === "redeemable" ? "phase_open" : "phase_limit_reached")}</td>
                <td data-label={say("campaignRedeemed")}>{say("campaignOf", { n: n.format(row.redeemed_count), limit: n.format(row.redemption_limit ?? 0) })}</td>
                <td data-label={say("campaignCodes")}>{say("campaignCodesValue", { available: n.format(row.available_codes), issued: n.format(row.issued_codes) })}</td>
              </tr>
            ))}</tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
