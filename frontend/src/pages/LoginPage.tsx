import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { GITHUB_LOGIN_URL, login } from "../lib/api";
import { setTokens } from "../lib/auth";

// parseOAuthFragment reads tokens (or an error) the GitHub OAuth callback put in the
// URL fragment so they never hit server logs.
function parseOAuthFragment() {
    if (!window.location.hash) {
        return { error: "", token: "", refreshToken: "" };
    }
    const fragment = new URLSearchParams(window.location.hash.slice(1));
    return {
        error: fragment.get("oauth_error") ?? "",
        token: fragment.get("token") ?? "",
        refreshToken: fragment.get("refresh_token") ?? "",
    };
}

export function LoginPage() {
    const navigate = useNavigate();
    const [oauthResult] = useState(parseOAuthFragment);
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [error, setError] = useState(oauthResult.error);
    const [isSubmitting, setIsSubmitting] = useState(false);

    useEffect(() => {
        if (window.location.hash) {
            window.history.replaceState(null, "", window.location.pathname);
        }
        if (oauthResult.token) {
            setTokens(oauthResult.token, oauthResult.refreshToken || undefined);
            navigate("/dashboard", { replace: true });
        }
    }, [oauthResult, navigate]);

    async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
        event.preventDefault();
        setError("");
        setIsSubmitting(true);

        try {
            const pair = await login(email.trim(), password);
            setTokens(pair.token, pair.refresh_token);
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

                <div className="mt-4 flex items-center gap-3">
                    <div className="h-px flex-1 bg-slate-800" />
                    <span className="text-xs uppercase tracking-wide text-slate-500">or</span>
                    <div className="h-px flex-1 bg-slate-800" />
                </div>

                <a
                    className="mt-4 flex w-full items-center justify-center gap-2 rounded-md border border-slate-700 px-3 py-2 text-sm font-medium text-slate-200 hover:bg-slate-800"
                    href={GITHUB_LOGIN_URL}
                >
                    <svg aria-hidden="true" className="h-4 w-4 fill-current" viewBox="0 0 16 16">
                        <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
                    </svg>
                    Continue with GitHub
                </a>

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
