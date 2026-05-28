import { Link } from "react-router-dom";

export function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-950 px-4 text-slate-100">
      <section className="w-full max-w-md rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <h1 className="text-2xl font-semibold">Sign in</h1>
        <p className="mt-2 text-sm text-slate-400">
          Access your remote development environments.
        </p>

        <form className="mt-6 space-y-4">
          <div>
            <label className="mb-1 block text-sm text-slate-300" htmlFor="email">
              Email
            </label>
            <input
              className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 outline-none ring-cyan-500 focus:ring"
              id="email"
              type="email"
              placeholder="you@example.com"
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
            />
          </div>
          <button
            className="w-full rounded-md bg-cyan-500 px-3 py-2 font-medium text-slate-950 hover:bg-cyan-400"
            type="button"
          >
            Sign in
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
