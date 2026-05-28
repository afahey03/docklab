import { useState } from "react";
import { Link } from "react-router-dom";
import { useNavigate } from "react-router-dom";
import { login } from "../lib/api";
import { setToken } from "../lib/auth";

export function LoginPage() {
    const navigate = useNavigate();
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [error, setError] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);

    async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
        event.preventDefault();
        setError("");
        setIsSubmitting(true);

        try {
            const token = await login(email.trim(), password);
            setToken(token);
            navigate("/dashboard", { replace: true });
        } catch (requestError) {
            setError(
                requestError instanceof Error
                    ? requestError.message
                    : "Unable to sign in",
            );
        } finally {
            setIsSubmitting(false);
        }
    }

    return (
        <main className="flex min-h-screen items-center justify-center bg-slate-950 px-4 text-slate-100">
            <section className="w-full max-w-md rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
                <h1 className="text-2xl font-semibold">Sign in</h1>
                <p className="mt-2 text-sm text-slate-400">
                    Access your remote development environments.
                </p>

                <form className="mt-6 space-y-4" onSubmit={handleSubmit}>
                    <div>
                        <label className="mb-1 block text-sm text-slate-300" htmlFor="email">
                            Email
                        </label>
                        <input
                            className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 outline-none ring-cyan-500 focus:ring"
                            id="email"
                            type="email"
                            placeholder="you@example.com"
                            value={email}
                            onChange={(event) => setEmail(event.target.value)}
                            required
                        />
                    </div>
                    <div>
                        <label className="mb-1 block text-sm text-slate-300" htmlFor="password">
                            Password
                        </label>
                        <input
                            className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 outline-none ring-cyan-500 focus:ring"
                            id="password"
                            type="password"
                            placeholder="••••••••"
                            value={password}
                            onChange={(event) => setPassword(event.target.value)}
                            minLength={8}
                            required
                        />
                    </div>
                    {error ? <p className="text-sm text-rose-400">{error}</p> : null}
                    <button
                        className="w-full rounded-md bg-cyan-500 px-3 py-2 font-medium text-slate-950 hover:bg-cyan-400"
                        type="submit"
                        disabled={isSubmitting}
                    >
                        {isSubmitting ? "Signing in..." : "Sign in"}
                    </button>
                </form>

                <p className="mt-4 text-sm text-slate-400">
                    No account?{" "}
                    <Link className="text-cyan-400 hover:text-cyan-300" to="/register">
                        Create one
                    </Link>
                </p>
            </section>
        </main>
    );
}
