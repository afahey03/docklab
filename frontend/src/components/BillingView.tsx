import { useCallback, useEffect, useState } from "react";
import {
    getBillingSummary,
    getUsage,
    updateBudget,
    type BillingSummary,
    type UsageSummary,
} from "../lib/api";

const currencyFormatter = new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
});

function formatCurrency(value: number): string {
    return currencyFormatter.format(value);
}

function formatMinutes(minutes: number): string {
    const total = Math.max(0, Math.round(minutes));
    const days = Math.floor(total / (24 * 60));
    const hours = Math.floor((total % (24 * 60)) / 60);
    const mins = total % 60;
    if (days > 0) {
        return `${days}d ${hours}h`;
    }
    if (hours > 0) {
        return `${hours}h ${mins}m`;
    }
    return `${mins}m`;
}

function formatTimestamp(value: string | null): string {
    if (!value) {
        return "running";
    }
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

type BillingViewProps = {
    onError: (message: string) => void;
    onNotice: (message: string) => void;
};

// BillingView shows server-tracked usage sessions, month-to-date cost rollups per
// environment, and the monthly budget editor.
export function BillingView({ onError, onNotice }: BillingViewProps) {
    const [usage, setUsage] = useState<UsageSummary | null>(null);
    const [billing, setBilling] = useState<BillingSummary | null>(null);
    const [budgetInput, setBudgetInput] = useState("");
    const [alertsEnabled, setAlertsEnabled] = useState(true);
    const [isLoading, setIsLoading] = useState(true);
    const [isSavingBudget, setIsSavingBudget] = useState(false);

    const refresh = useCallback(async () => {
        try {
            const [usageSummary, billingSummary] = await Promise.all([getUsage(), getBillingSummary()]);
            setUsage(usageSummary);
            setBilling(billingSummary);
            setBudgetInput(
                billingSummary.monthly_budget_usd > 0 ? String(billingSummary.monthly_budget_usd) : "",
            );
            setAlertsEnabled(billingSummary.settings?.budget_alerts_enabled ?? true);
        } catch (requestError) {
            onError(requestError instanceof Error ? requestError.message : "failed to load billing data");
        } finally {
            setIsLoading(false);
        }
    }, [onError]);

    useEffect(() => {
        async function load() {
            await refresh();
        }
        void load();
    }, [refresh]);

    async function handleSaveBudget() {
        const parsed = budgetInput.trim() === "" ? 0 : Number(budgetInput);
        if (Number.isNaN(parsed) || parsed < 0) {
            onError("monthly budget must be a non-negative number");
            return;
        }

        setIsSavingBudget(true);
        try {
            await updateBudget(parsed, alertsEnabled);
            onNotice("budget settings saved");
            await refresh();
        } catch (requestError) {
            onError(requestError instanceof Error ? requestError.message : "failed to save budget");
        } finally {
            setIsSavingBudget(false);
        }
    }

    if (isLoading) {
        return (
            <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                <p className="text-sm text-slate-400">Loading usage data...</p>
            </article>
        );
    }

    const maxEnvCost = Math.max(0.01, ...(billing?.by_environment.map((item) => item.cost_usd) ?? []));

    return (
        <>
            <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                <div className="flex items-center justify-between gap-3">
                    <div>
                        <h3 className="font-medium">Usage & cost overview</h3>
                        <p className="mt-1 text-sm text-slate-400">
                            Tracked EC2 runtime sessions priced via the AWS Pricing API (or static fallback rates).
                        </p>
                    </div>
                    <p className="text-xs text-slate-500">Estimates, not AWS invoices.</p>
                </div>

                <div className="mt-4 grid gap-3 md:grid-cols-4">
                    <div className="rounded-md border border-slate-800 bg-slate-950 p-3">
                        <p className="text-xs uppercase tracking-wide text-slate-500">Month to date ({billing?.month})</p>
                        <p className="mt-2 text-2xl font-semibold text-slate-100">
                            {formatCurrency(billing?.month_to_date_usd ?? 0)}
                        </p>
                    </div>
                    <div className="rounded-md border border-slate-800 bg-slate-950 p-3">
                        <p className="text-xs uppercase tracking-wide text-slate-500">All-time tracked spend</p>
                        <p className="mt-2 text-2xl font-semibold text-slate-100">
                            {formatCurrency(usage?.total_cost_usd ?? 0)}
                        </p>
                    </div>
                    <div className="rounded-md border border-slate-800 bg-slate-950 p-3">
                        <p className="text-xs uppercase tracking-wide text-slate-500">Open sessions</p>
                        <p className="mt-2 text-2xl font-semibold text-slate-100">{usage?.open_session_count ?? 0}</p>
                    </div>
                    <div
                        className={`rounded-md border p-3 ${billing?.over_budget ? "border-rose-800 bg-rose-950/30" : "border-slate-800 bg-slate-950"}`}
                    >
                        <p className="text-xs uppercase tracking-wide text-slate-500">Budget used</p>
                        <p className={`mt-2 text-2xl font-semibold ${billing?.over_budget ? "text-rose-300" : "text-slate-100"}`}>
                            {billing && billing.monthly_budget_usd > 0
                                ? `${billing.budget_used_pct.toFixed(0)}%`
                                : "No budget"}
                        </p>
                        {billing?.over_budget ? (
                            <p className="mt-1 text-xs text-rose-400">Monthly budget exceeded.</p>
                        ) : null}
                    </div>
                </div>
            </article>

            <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                <h3 className="font-medium">Monthly budget</h3>
                <p className="mt-1 text-sm text-slate-400">
                    Set a monthly cloud spend budget. When alerts are enabled and a webhook is configured on the
                    server, exceeding the budget raises an alert.
                </p>
                <div className="mt-3 flex flex-wrap items-center gap-3">
                    <input
                        className="w-40 rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-slate-100 placeholder:text-slate-500"
                        placeholder="e.g. 25.00"
                        inputMode="decimal"
                        value={budgetInput}
                        onChange={(event) => setBudgetInput(event.target.value)}
                    />
                    <label className="flex items-center gap-2 text-sm text-slate-300">
                        <input
                            checked={alertsEnabled}
                            className="h-4 w-4 accent-cyan-500"
                            type="checkbox"
                            onChange={(event) => setAlertsEnabled(event.target.checked)}
                        />
                        Budget alerts
                    </label>
                    <button
                        className="rounded-md bg-cyan-500 px-3 py-1.5 text-sm font-medium text-slate-950 hover:bg-cyan-400 disabled:opacity-60"
                        type="button"
                        disabled={isSavingBudget}
                        onClick={() => void handleSaveBudget()}
                    >
                        {isSavingBudget ? "Saving..." : "Save budget"}
                    </button>
                </div>
            </article>

            <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                <h3 className="font-medium">This month by environment</h3>
                {!billing || billing.by_environment.length === 0 ? (
                    <p className="mt-2 text-sm text-slate-400">No tracked cloud usage this month.</p>
                ) : (
                    <div className="mt-3 space-y-2">
                        {billing.by_environment.map((item) => (
                            <div key={item.environment_id} className="rounded-md border border-slate-800 bg-slate-950 p-3">
                                <div className="flex flex-wrap items-center justify-between gap-2">
                                    <div>
                                        <p className="text-sm font-medium text-slate-100">
                                            {item.environment_name}
                                            {item.open ? (
                                                <span className="ml-2 rounded-full border border-emerald-800 px-2 py-0.5 text-[11px] text-emerald-300">
                                                    running
                                                </span>
                                            ) : null}
                                        </p>
                                        <p className="text-xs text-slate-500">
                                            {item.instance_type || "unknown type"}
                                            {item.region ? ` | ${item.region}` : ""} | {formatMinutes(item.runtime_minutes)}
                                        </p>
                                    </div>
                                    <p className="text-sm font-semibold text-slate-100">{formatCurrency(item.cost_usd)}</p>
                                </div>
                                <div className="mt-2 h-2 overflow-hidden rounded-full bg-slate-800">
                                    <div
                                        className="h-full rounded-full bg-cyan-500"
                                        style={{ width: `${Math.min(100, (item.cost_usd / maxEnvCost) * 100)}%` }}
                                    />
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </article>

            <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                <h3 className="font-medium">Usage history</h3>
                <p className="mt-1 text-sm text-slate-400">Recent EC2 runtime sessions (most recent first).</p>
                {!usage || usage.sessions.length === 0 ? (
                    <p className="mt-2 text-sm text-slate-400">No usage sessions recorded yet.</p>
                ) : (
                    <div className="mt-3 overflow-x-auto">
                        <table className="w-full text-left text-xs">
                            <thead className="text-slate-500">
                                <tr>
                                    <th className="pb-2 pr-3 font-medium">Environment</th>
                                    <th className="pb-2 pr-3 font-medium">Instance</th>
                                    <th className="pb-2 pr-3 font-medium">Started</th>
                                    <th className="pb-2 pr-3 font-medium">Ended</th>
                                    <th className="pb-2 pr-3 font-medium">Runtime</th>
                                    <th className="pb-2 pr-3 font-medium">Rate</th>
                                    <th className="pb-2 font-medium">Cost</th>
                                </tr>
                            </thead>
                            <tbody className="text-slate-300">
                                {usage.sessions.map((session) => (
                                    <tr key={session.id} className="border-t border-slate-800">
                                        <td className="py-2 pr-3">{session.environment_name}</td>
                                        <td className="py-2 pr-3">
                                            {session.instance_type}
                                            {session.region ? ` (${session.region})` : ""}
                                        </td>
                                        <td className="py-2 pr-3">{formatTimestamp(session.started_at)}</td>
                                        <td className="py-2 pr-3">{formatTimestamp(session.ended_at)}</td>
                                        <td className="py-2 pr-3">{formatMinutes(session.runtime_minutes)}</td>
                                        <td className="py-2 pr-3">{formatCurrency(session.hourly_rate_usd)}/hr</td>
                                        <td className="py-2">{formatCurrency(session.estimated_cost_usd)}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </article>
        </>
    );
}
