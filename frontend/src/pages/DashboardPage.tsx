import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { getMe } from "../lib/api";
import { clearToken } from "../lib/auth";

export function DashboardPage() {
    const navigate = useNavigate();
    const [email, setEmail] = useState("");

    useEffect(() => {
        async function loadUser() {
            try {
                const user = await getMe();
                setEmail(user.email);
            } catch {
                clearToken();
                navigate("/login", { replace: true });
            }
        }

        void loadUser();
    }, [navigate]);

    function handleSignOut() {
        clearToken();
        navigate("/login", { replace: true });
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
                        <h3 className="font-medium">No environments yet</h3>
                        <p className="mt-1 text-sm text-slate-400">
                            Create your first isolated workspace from this dashboard.
                        </p>
                        <button
                            className="mt-4 rounded-md bg-cyan-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-cyan-400"
                            type="button"
                        >
                            New environment
                        </button>
                    </article>
                </section>
            </div>
        </main>
    );
}
