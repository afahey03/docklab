import { getToken } from "./auth";

const API_BASE_URL =
    import.meta.env.VITE_API_BASE_URL?.toString() ?? "http://localhost:8080";

type ErrorBody = {
    error?: string;
};

type AuthResponse = {
    token: string;
};

type MeResponse = {
    email: string;
};

async function readError(response: Response): Promise<string> {
    try {
        const body = (await response.json()) as ErrorBody;
        if (typeof body.error === "string" && body.error.length > 0) {
            return body.error;
        }
    } catch {
        // Ignore parse failures and fall back to status text.
    }

    return response.statusText || "request failed";
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(`${API_BASE_URL}${path}`, {
        headers: {
            "Content-Type": "application/json",
            ...(init?.headers ?? {}),
        },
        ...init,
    });

    if (!response.ok) {
        throw new Error(await readError(response));
    }

    return (await response.json()) as T;
}

export async function login(email: string, password: string): Promise<string> {
    const body = await request<AuthResponse>("/api/v1/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
    });

    return body.token;
}

export async function register(email: string, password: string): Promise<string> {
    const body = await request<AuthResponse>("/api/v1/auth/register", {
        method: "POST",
        body: JSON.stringify({ email, password }),
    });

    return body.token;
}

export async function getMe(): Promise<MeResponse> {
    const token = getToken();
    if (!token) {
        throw new Error("missing auth token");
    }

    return request<MeResponse>("/api/v1/auth/me", {
        method: "GET",
        headers: {
            Authorization: `Bearer ${token}`,
        },
    });
}
