import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
    createEnvironment,
    deleteEnvironment,
    getEnvironments,
    getMe,
    startEnvironment,
    stopEnvironment,
    type Environment,
} from "../lib/api";
import { clearToken } from "../lib/auth";

export function DashboardPage() {
    const navigate = useNavigate();
    const [email, setEmail] = useState("");
    const [environments, setEnvironments] = useState<Environment[]>([]);
    const [name, setName] = useState("");
    const [image, setImage] = useState("alpine:3.20");
    const [error, setError] = useState("");
    const [isBusy, setIsBusy] = useState(false);

    useEffect(() => {
        async function bootstrapDashboard() {
            try {
                const [user, envs] = await Promise.all([getMe(), getEnvironments()]);
                setEmail(user.email);
                setEnvironments(envs);
            } catch {
                clearToken();
                navigate("/login", { replace: true });
            }
        }

        void bootstrapDashboard();
    }, [navigate]);

    function handleSignOut() {
        clearToken();
        navigate("/login", { replace: true });
    }

    async function refreshEnvironments() {
        const envs = await getEnvironments();
        setEnvironments(envs);
    }

    async function handleCreateEnvironment() {
        setError("");
        setIsBusy(true);
        try {
            await createEnvironment(name, image);
            setName("");
            await refreshEnvironments();
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to create environment");
        } finally {
            setIsBusy(false);
        }
    }

    async function handleStartEnvironment(id: string) {
        setError("");
        setIsBusy(true);
        try {
            await startEnvironment(id);
            await refreshEnvironments();
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to start environment");
        } finally {
            setIsBusy(false);
        }
    }

    async function handleStopEnvironment(id: string) {
        setError("");
        setIsBusy(true);
        try {
            await stopEnvironment(id);
            await refreshEnvironments();
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to stop environment");
        } finally {
            setIsBusy(false);
        }
    }

    async function handleDeleteEnvironment(id: string) {
        setError("");
        setIsBusy(true);
        try {
            await deleteEnvironment(id);
            await refreshEnvironments();
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to delete environment");
        } finally {
            setIsBusy(false);
        }
    }

    return (
        <main className="min-h-screen bg-slate-950 text-slate-100">
            <div className="mx-auto grid max-w-6xl gap-6 p-6 md:grid-cols-[240px_1fr]">
                <aside className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                    <h2 className="text-lg font-semibold">DockLab</h2>
                    <nav className="mt-4 space-y-2 text-sm text-slate-300">
                        <p className="rounded-md bg-slate-800 px-3 py-2">Environments</p>
                        <p className="rounded-md px-3 py-2">Usage & Cost</p>
                        <p className="rounded-md px-3 py-2">Settings</p>
                    </nav>
                </aside>

                <section className="space-y-4">
                    <header className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                        <h1 className="text-xl font-semibold">Dashboard</h1>
                        <p className="mt-1 text-sm text-slate-400">
                            Launch and manage remote development environments.
                        </p>
                        <div className="mt-3 flex items-center justify-between">
                            <p className="text-sm text-slate-300">Signed in as {email || "loading..."}</p>
                            <button
                                className="rounded-md border border-slate-700 px-3 py-1 text-sm text-slate-200 hover:bg-slate-800"
                                type="button"
                                onClick={handleSignOut}
                            >
                                Sign out
                            </button>
                        </div>
                    </header>

                    <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                        <h3 className="font-medium">Create environment</h3>
                        <p className="mt-1 text-sm text-slate-400">Launch a local Docker workspace for your user.</p>

                        <div className="mt-4 grid gap-3 md:grid-cols-[1fr_1fr_auto]">
                            <input
                                className="rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500 focus:ring"
                                placeholder="Environment name (optional)"
                                value={name}
                                onChange={(event) => setName(event.target.value)}
                            />
                            <input
                                className="rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500 focus:ring"
                                placeholder="Docker image"
                                value={image}
                                onChange={(event) => setImage(event.target.value)}
                            />
                            <button
                                className="rounded-md bg-cyan-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-cyan-400 disabled:opacity-60"
                                type="button"
                                disabled={isBusy}
                                onClick={handleCreateEnvironment}
                            >
                                {isBusy ? "Working..." : "Create"}
                            </button>
                        </div>
                        {error ? <p className="mt-3 text-sm text-rose-400">{error}</p> : null}
                    </article>

                    <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                        <h3 className="font-medium">Your environments</h3>
                        {environments.length === 0 ? (
                            <p className="mt-1 text-sm text-slate-400">No environments yet.</p>
                        ) : (
                            <div className="mt-3 space-y-3">
                                {environments.map((env) => (
                                    <div
                                        key={env.id}
                                        className="rounded-md border border-slate-800 bg-slate-950 p-3"
                                    >
                                        <div className="flex flex-wrap items-center justify-between gap-2">
                                            <div>
                                                <p className="font-medium text-slate-100">{env.name}</p>
                                                <p className="text-xs text-slate-400">{env.image}</p>
                                            </div>
                                            <span className="rounded-full border border-slate-700 px-2 py-1 text-xs text-slate-300">
                                                {env.status}
                                            </span>
                                        </div>

                                        <div className="mt-3 flex flex-wrap gap-2">
                                            <button
                                                className="rounded-md border border-emerald-700 px-3 py-1 text-xs text-emerald-300 hover:bg-emerald-950 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                                                type="button"
                                                disabled={isBusy || env.status === "running"}
                                                onClick={() => handleStartEnvironment(env.id)}
                                            >
                                                Start
                                            </button>
                                            <button
                                                className="rounded-md border border-amber-700 px-3 py-1 text-xs text-amber-300 hover:bg-amber-950 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                                                type="button"
                                                disabled={isBusy || env.status !== "running"}
                                                onClick={() => handleStopEnvironment(env.id)}
                                            >
                                                Stop
                                            </button>
                                            <button
                                                className="rounded-md border border-rose-700 px-3 py-1 text-xs text-rose-300 hover:bg-rose-950"
                                                type="button"
                                                disabled={isBusy}
                                                onClick={() => handleDeleteEnvironment(env.id)}
                                            >
                                                Delete
                                            </button>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </article>
                </section>
            </div>
        </main>
    );
}
