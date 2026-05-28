import { useState } from "react";
import { Link } from "react-router-dom";
import { useNavigate } from "react-router-dom";
import { register } from "../lib/api";
import { setToken } from "../lib/auth";

export function RegisterPage() {
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
            const token = await register(email.trim(), password);
            setToken(token);
            navigate("/dashboard", { replace: true });
        } catch (requestError) {
            setError(
                requestError instanceof Error
                    ? requestError.message
                    : "Unable to create account",
            );
        } finally {
            setIsSubmitting(false);
        }
    }

    return (
        <main className="flex min-h-screen items-center justify-center bg-slate-950 px-4 text-slate-100">
            <section className="w-full max-w-md rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
                <h1 className="text-2xl font-semibold">Create account</h1>
                <p className="mt-2 text-sm text-slate-400">
                    Set up your DockLab account to launch dev workspaces.
                </p>

                <form className="mt-6 space-y-4" onSubmit={handleSubmit}>
                    <input
                        className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 outline-none ring-cyan-500 focus:ring"
                        placeholder="Email"
                        type="email"
                        value={email}
                        onChange={(event) => setEmail(event.target.value)}
                        required
                    />
                    <input
                        className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 outline-none ring-cyan-500 focus:ring"
                        placeholder="Password"
                        type="password"
                        value={password}
                        onChange={(event) => setPassword(event.target.value)}
                        minLength={8}
                        required
                    />
                    {error ? <p className="text-sm text-rose-400">{error}</p> : null}
                    <button
                        className="w-full rounded-md bg-cyan-500 px-3 py-2 font-medium text-slate-950 hover:bg-cyan-400"
                        type="submit"
                        disabled={isSubmitting}
                    >
                        {isSubmitting ? "Creating account..." : "Create account"}
                    </button>
                </form>

                <p className="mt-4 text-sm text-slate-400">
                    Already registered?{" "}
                    <Link className="text-cyan-400 hover:text-cyan-300" to="/login">
                        Sign in
                    </Link>
                </p>
            </section>
        </main>
    );
}
