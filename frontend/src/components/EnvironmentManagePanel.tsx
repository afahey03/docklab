import { useCallback, useEffect, useState } from "react";
import {
    createSnapshot,
    deleteSnapshot,
    getIDEStatus,
    getShares,
    getSnapshots,
    restoreSnapshot,
    shareEnvironment,
    startIDE,
    stopIDE,
    unshareEnvironment,
    type Environment,
    type EnvironmentShare,
    type EnvironmentSnapshot,
    type IDEStatus,
} from "../lib/api";

type PanelProps = {
    environment: Environment;
    onEnvironmentChanged: () => void;
    onError: (message: string) => void;
    onNotice: (message: string) => void;
};

function formatTimestamp(value: string): string {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

// EnvironmentManagePanel groups the per-environment power features: workspace
// snapshots, sharing with other users, and the browser IDE sidecar.
export function EnvironmentManagePanel({ environment, onEnvironmentChanged, onError, onNotice }: PanelProps) {
    const [snapshots, setSnapshots] = useState<EnvironmentSnapshot[]>([]);
    const [shares, setShares] = useState<EnvironmentShare[]>([]);
    const [ideStatus, setIdeStatus] = useState<IDEStatus | null>(null);
    const [snapshotNote, setSnapshotNote] = useState("");
    const [shareEmail, setShareEmail] = useState("");
    const [busy, setBusy] = useState<string>("");

    const environmentId = environment.id;

    const refresh = useCallback(async () => {
        const results = await Promise.allSettled([
            getSnapshots(environmentId),
            getShares(environmentId),
            getIDEStatus(environmentId),
        ]);
        if (results[0].status === "fulfilled") {
            setSnapshots(results[0].value);
        }
        if (results[1].status === "fulfilled") {
            setShares(results[1].value);
        }
        if (results[2].status === "fulfilled") {
            setIdeStatus(results[2].value);
        } else {
            setIdeStatus(null);
        }
    }, [environmentId]);

    useEffect(() => {
        async function load() {
            await refresh();
        }
        void load();
    }, [refresh]);

    async function run(action: string, fn: () => Promise<void>) {
        setBusy(action);
        try {
            await fn();
        } catch (requestError) {
            onError(requestError instanceof Error ? requestError.message : `failed to ${action}`);
        } finally {
            setBusy("");
            void refresh();
        }
    }

    return (
        <div className="mt-3 grid gap-3 rounded-md border border-slate-800 bg-slate-900/60 p-3 lg:grid-cols-3">
            <section>
                <h4 className="text-sm font-medium text-slate-200">Snapshots</h4>
                <p className="mt-1 text-xs text-slate-500">
                    Save the workspace filesystem as an image and restore it later.
                </p>
                <div className="mt-2 flex gap-2">
                    <input
                        className="min-w-0 flex-1 rounded-md border border-slate-700 bg-slate-950 px-2 py-1 text-xs text-slate-100 placeholder:text-slate-500"
                        placeholder="Snapshot note (optional)"
                        value={snapshotNote}
                        onChange={(event) => setSnapshotNote(event.target.value)}
                        maxLength={128}
                    />
                    <button
                        className="rounded-md border border-cyan-700 px-2 py-1 text-xs text-cyan-300 hover:bg-cyan-950 disabled:opacity-50"
                        type="button"
                        disabled={busy !== "" || environment.status !== "running"}
                        onClick={() =>
                            void run("create snapshot", async () => {
                                await createSnapshot(environmentId, snapshotNote.trim());
                                setSnapshotNote("");
                                onNotice("snapshot created");
                            })
                        }
                    >
                        {busy === "create snapshot" ? "Saving..." : "Snapshot"}
                    </button>
                </div>
                {environment.status !== "running" ? (
                    <p className="mt-1 text-xs text-slate-500">Workspace must be running to snapshot.</p>
                ) : null}
                <ul className="mt-2 space-y-2">
                    {snapshots.length === 0 ? (
                        <li className="text-xs text-slate-500">No snapshots yet.</li>
                    ) : (
                        snapshots.map((snapshot) => (
                            <li key={snapshot.id} className="rounded-md border border-slate-800 bg-slate-950 p-2">
                                <p className="text-xs text-slate-300">{snapshot.note || "(no note)"}</p>
                                <p className="text-[11px] text-slate-500">{formatTimestamp(snapshot.created_at)}</p>
                                <div className="mt-1 flex gap-2">
                                    <button
                                        className="rounded border border-emerald-800 px-2 py-0.5 text-[11px] text-emerald-300 hover:bg-emerald-950 disabled:opacity-50"
                                        type="button"
                                        disabled={busy !== ""}
                                        onClick={() =>
                                            void run("restore snapshot", async () => {
                                                await restoreSnapshot(environmentId, snapshot.id);
                                                onNotice("snapshot restored; workspace recreated");
                                                onEnvironmentChanged();
                                            })
                                        }
                                    >
                                        Restore
                                    </button>
                                    <button
                                        className="rounded border border-rose-800 px-2 py-0.5 text-[11px] text-rose-300 hover:bg-rose-950 disabled:opacity-50"
                                        type="button"
                                        disabled={busy !== ""}
                                        onClick={() =>
                                            void run("delete snapshot", async () => {
                                                await deleteSnapshot(environmentId, snapshot.id);
                                                onNotice("snapshot deleted");
                                            })
                                        }
                                    >
                                        Delete
                                    </button>
                                </div>
                            </li>
                        ))
                    )}
                </ul>
            </section>

            <section>
                <h4 className="text-sm font-medium text-slate-200">Sharing</h4>
                <p className="mt-1 text-xs text-slate-500">
                    Grant another DockLab user terminal access to this environment.
                </p>
                <div className="mt-2 flex gap-2">
                    <input
                        className="min-w-0 flex-1 rounded-md border border-slate-700 bg-slate-950 px-2 py-1 text-xs text-slate-100 placeholder:text-slate-500"
                        placeholder="teammate@example.com"
                        type="email"
                        value={shareEmail}
                        onChange={(event) => setShareEmail(event.target.value)}
                    />
                    <button
                        className="rounded-md border border-indigo-700 px-2 py-1 text-xs text-indigo-300 hover:bg-indigo-950 disabled:opacity-50"
                        type="button"
                        disabled={busy !== "" || !shareEmail.trim()}
                        onClick={() =>
                            void run("share environment", async () => {
                                await shareEnvironment(environmentId, shareEmail.trim());
                                setShareEmail("");
                                onNotice("environment shared");
                            })
                        }
                    >
                        {busy === "share environment" ? "Sharing..." : "Share"}
                    </button>
                </div>
                <ul className="mt-2 space-y-2">
                    {shares.length === 0 ? (
                        <li className="text-xs text-slate-500">Not shared with anyone.</li>
                    ) : (
                        shares.map((share) => (
                            <li
                                key={share.id}
                                className="flex items-center justify-between rounded-md border border-slate-800 bg-slate-950 p-2"
                            >
                                <span className="text-xs text-slate-300">{share.shared_with_email}</span>
                                <button
                                    className="rounded border border-rose-800 px-2 py-0.5 text-[11px] text-rose-300 hover:bg-rose-950 disabled:opacity-50"
                                    type="button"
                                    disabled={busy !== ""}
                                    onClick={() =>
                                        void run("revoke share", async () => {
                                            await unshareEnvironment(environmentId, share.shared_with_email);
                                            onNotice("share revoked");
                                        })
                                    }
                                >
                                    Revoke
                                </button>
                            </li>
                        ))
                    )}
                </ul>
            </section>

            <section>
                <h4 className="text-sm font-medium text-slate-200">Browser IDE</h4>
                <p className="mt-1 text-xs text-slate-500">
                    Run VS Code (code-server) against this workspace's files.
                </p>
                <div className="mt-2 flex gap-2">
                    <button
                        className="rounded-md border border-emerald-700 px-2 py-1 text-xs text-emerald-300 hover:bg-emerald-950 disabled:opacity-50"
                        type="button"
                        disabled={busy !== "" || environment.status !== "running" || Boolean(ideStatus?.running)}
                        onClick={() =>
                            void run("start IDE", async () => {
                                const status = await startIDE(environmentId);
                                setIdeStatus(status);
                                onNotice("browser IDE started");
                            })
                        }
                    >
                        {busy === "start IDE" ? "Starting..." : "Start IDE"}
                    </button>
                    <button
                        className="rounded-md border border-rose-700 px-2 py-1 text-xs text-rose-300 hover:bg-rose-950 disabled:opacity-50"
                        type="button"
                        disabled={busy !== "" || !ideStatus?.running}
                        onClick={() =>
                            void run("stop IDE", async () => {
                                await stopIDE(environmentId);
                                setIdeStatus({ running: false });
                                onNotice("browser IDE stopped");
                            })
                        }
                    >
                        {busy === "stop IDE" ? "Stopping..." : "Stop IDE"}
                    </button>
                </div>
                {ideStatus?.running ? (
                    <div className="mt-2 rounded-md border border-slate-800 bg-slate-950 p-2 text-xs">
                        {ideStatus.url ? (
                            <p className="text-slate-300">
                                URL:{" "}
                                <a
                                    className="text-cyan-400 hover:text-cyan-300"
                                    href={ideStatus.url}
                                    rel="noreferrer"
                                    target="_blank"
                                >
                                    {ideStatus.url}
                                </a>
                            </p>
                        ) : null}
                        {ideStatus.password ? (
                            <p className="mt-1 break-all text-slate-400">
                                Password: <code className="text-slate-200">{ideStatus.password}</code>
                            </p>
                        ) : null}
                    </div>
                ) : (
                    <p className="mt-2 text-xs text-slate-500">
                        {environment.status === "running" ? "IDE is not running." : "Workspace must be running."}
                    </p>
                )}
            </section>
        </div>
    );
}
