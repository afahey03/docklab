import { clearToken, getRefreshToken, getToken, setTokens } from "./auth";

const API_BASE_URL =
    import.meta.env.VITE_API_BASE_URL?.toString() ?? "http://localhost:8080";

export const GITHUB_LOGIN_URL = `${API_BASE_URL}/api/v1/auth/github/login`;

type ErrorBody = {
    code?: string;
    error?: string;
};

export type TokenPair = {
    token: string;
    refresh_token?: string;
};

type MeResponse = {
    email: string;
};

export type Environment = {
    id: string;
    user_email: string;
    name: string;
    image: string;
    status: string;
    container_id: string;
    creation_mode: string;
    repo_url: string;
    template_id: string;
    runtime_target: string;
    cloud_status: string;
    cloud_region: string;
    cloud_instance_type: string;
    cloud_key_name: string;
    instance_id: string;
    public_ip: string;
    terraform_dir: string;
    cloud_error: string;
    cloud_provisioned_at: string | null;
    last_activity_at: string;
    created_at: string;
    updated_at: string;
};

export type LifecyclePolicy = {
    enabled: boolean;
    workspace_idle_stop_minutes: number;
    cloud_idle_stop_minutes: number;
    cloud_idle_terminate_minutes: number;
};

export type RemoteHealthStatus = {
    runtime_target: string;
    public_ip: string;
    ssh_reachable: boolean;
    docker_available: boolean;
    workspace_ready?: boolean;
    error?: string;
};

export type EnvironmentTemplate = {
    id: string;
    name: string;
    description: string;
    image: string;
    language: string;
};

export type EnvironmentShare = {
    id: string;
    environment_id: string;
    owner_email: string;
    shared_with_email: string;
    created_at: string;
};

export type EnvironmentSnapshot = {
    id: string;
    environment_id: string;
    user_email: string;
    image_tag: string;
    note: string;
    runtime_target: string;
    created_at: string;
};

export type IDEStatus = {
    running: boolean;
    url?: string;
    password?: string;
};

export type UsageSession = {
    id: string;
    environment_id: string;
    user_email: string;
    environment_name: string;
    instance_type: string;
    region: string;
    hourly_rate_usd: number;
    started_at: string;
    ended_at: string | null;
    runtime_minutes: number;
    estimated_cost_usd: number;
};

export type UsageSummary = {
    sessions: UsageSession[];
    total_cost_usd: number;
    month_to_date_usd: number;
    open_session_count: number;
    total_session_count: number;
};

export type UserSettings = {
    user_email: string;
    monthly_budget_usd: number;
    budget_alerts_enabled: boolean;
    updated_at: string;
};

export type EnvironmentBillItem = {
    environment_id: string;
    environment_name: string;
    instance_type: string;
    region: string;
    runtime_minutes: number;
    cost_usd: number;
    open: boolean;
};

export type BillingSummary = {
    month: string;
    month_to_date_usd: number;
    monthly_budget_usd: number;
    over_budget: boolean;
    budget_used_pct: number;
    settings: UserSettings | null;
    by_environment: EnvironmentBillItem[];
};

export type CreateEnvironmentRequest = {
    name?: string;
    image: string;
    target?: "local" | "cloud";
    repo_url?: string;
    template_id?: string;
    provision?: ProvisionRequest;
};

export type CreateEnvironmentResponse = {
    environment: Environment;
    operation?: Operation;
};

export type ProvisionRequest = {
    region: string;
    instance_type: string;
    ami: string;
    key_name: string;
};

export type Operation = {
    id: string;
    user_email: string;
    environment_id: string;
    type: string;
    status: "queued" | "running" | "succeeded" | "failed";
    error: string;
    created_at: string;
    updated_at: string;
};

type EnvironmentsResponse = {
    environments: Environment[];
    shared_environments?: Environment[];
};

export type EnvironmentList = {
    environments: Environment[];
    sharedEnvironments: Environment[];
};

function authHeader(): Record<string, string> {
    const token = getToken();
    if (!token) {
        throw new Error("missing auth token");
    }
    return { Authorization: `Bearer ${token}` };
}

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

    if (response.status === 204) {
        return undefined as T;
    }

    return (await response.json()) as T;
}

let refreshPromise: Promise<boolean> | null = null;

// tryRefreshSession exchanges the stored refresh token for a new token pair.
// Concurrent 401s share a single in-flight refresh.
async function tryRefreshSession(): Promise<boolean> {
    if (refreshPromise) {
        return refreshPromise;
    }

    const refreshToken = getRefreshToken();
    if (!refreshToken) {
        return false;
    }

    refreshPromise = (async () => {
        try {
            const pair = await request<TokenPair>("/api/v1/auth/refresh", {
                method: "POST",
                body: JSON.stringify({ refresh_token: refreshToken }),
            });
            setTokens(pair.token, pair.refresh_token);
            return true;
        } catch {
            clearToken();
            return false;
        } finally {
            refreshPromise = null;
        }
    })();

    return refreshPromise;
}

// authedRequest performs an authenticated request and transparently retries once
// after refreshing an expired access token.
async function authedRequest<T>(path: string, init?: RequestInit): Promise<T> {
    const doFetch = () =>
        fetch(`${API_BASE_URL}${path}`, {
            ...init,
            headers: {
                "Content-Type": "application/json",
                ...authHeader(),
                ...(init?.headers ?? {}),
            },
        });

    let response = await doFetch();

    if (response.status === 401 && (await tryRefreshSession())) {
        response = await doFetch();
    }

    if (!response.ok) {
        throw new Error(await readError(response));
    }

    if (response.status === 204) {
        return undefined as T;
    }

    return (await response.json()) as T;
}

export async function login(email: string, password: string): Promise<TokenPair> {
    return request<TokenPair>("/api/v1/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
    });
}

export async function register(email: string, password: string): Promise<TokenPair> {
    return request<TokenPair>("/api/v1/auth/register", {
        method: "POST",
        body: JSON.stringify({ email, password }),
    });
}

export async function logout(): Promise<void> {
    const refreshToken = getRefreshToken();
    if (!refreshToken) {
        return;
    }
    try {
        await request<void>("/api/v1/auth/logout", {
            method: "POST",
            body: JSON.stringify({ refresh_token: refreshToken }),
        });
    } catch {
        // Best effort: local sign-out proceeds even if revocation fails.
    }
}

export async function getMe(): Promise<MeResponse> {
    return authedRequest<MeResponse>("/api/v1/auth/me", { method: "GET" });
}

export async function getEnvironments(): Promise<EnvironmentList> {
    const body = await authedRequest<EnvironmentsResponse>("/api/v1/environments", {
        method: "GET",
    });

    return {
        environments: body.environments ?? [],
        sharedEnvironments: body.shared_environments ?? [],
    };
}

export async function getTemplates(): Promise<EnvironmentTemplate[]> {
    const body = await authedRequest<{ templates: EnvironmentTemplate[] }>("/api/v1/templates", {
        method: "GET",
    });
    return body.templates ?? [];
}

export async function createEnvironment(payload: CreateEnvironmentRequest): Promise<CreateEnvironmentResponse> {
    return authedRequest<CreateEnvironmentResponse>("/api/v1/environments", {
        method: "POST",
        body: JSON.stringify({
            name: payload.name?.trim() ?? "",
            image: payload.image.trim(),
            target: payload.target ?? "local",
            repo_url: payload.repo_url?.trim() ?? "",
            template_id: payload.template_id ?? "",
            provision: payload.provision,
        }),
    });
}

export async function startEnvironment(id: string): Promise<Environment> {
    return authedRequest<Environment>(`/api/v1/environments/${id}/start`, { method: "POST" });
}

export async function stopEnvironment(id: string): Promise<Environment> {
    return authedRequest<Environment>(`/api/v1/environments/${id}/stop`, { method: "POST" });
}

export async function deleteEnvironment(id: string): Promise<Operation> {
    return authedRequest<Operation>(`/api/v1/environments/${id}`, { method: "DELETE" });
}

export async function provisionEnvironment(id: string, payload: ProvisionRequest): Promise<Operation> {
    return authedRequest<Operation>(`/api/v1/environments/${id}/provision`, {
        method: "POST",
        body: JSON.stringify(payload),
    });
}

export async function destroyCloudEnvironment(id: string): Promise<Operation> {
    return authedRequest<Operation>(`/api/v1/environments/${id}/destroy-cloud`, { method: "POST" });
}

export async function retryRemoteBootstrap(id: string): Promise<Operation> {
    return authedRequest<Operation>(`/api/v1/environments/${id}/retry-bootstrap`, { method: "POST" });
}

export async function getRemoteHealth(id: string): Promise<RemoteHealthStatus> {
    return authedRequest<RemoteHealthStatus>(`/api/v1/environments/${id}/remote-health`, { method: "GET" });
}

export async function getLifecyclePolicy(): Promise<LifecyclePolicy> {
    return authedRequest<LifecyclePolicy>("/api/v1/lifecycle-policy", { method: "GET" });
}

export async function getOperation(id: string): Promise<Operation> {
    return authedRequest<Operation>(`/api/v1/operations/${id}`, { method: "GET" });
}

// --- Snapshots ---

export async function createSnapshot(environmentId: string, note: string): Promise<EnvironmentSnapshot> {
    return authedRequest<EnvironmentSnapshot>(`/api/v1/environments/${environmentId}/snapshots`, {
        method: "POST",
        body: JSON.stringify({ note }),
    });
}

export async function getSnapshots(environmentId: string): Promise<EnvironmentSnapshot[]> {
    const body = await authedRequest<{ snapshots: EnvironmentSnapshot[] }>(
        `/api/v1/environments/${environmentId}/snapshots`,
        { method: "GET" },
    );
    return body.snapshots ?? [];
}

export async function restoreSnapshot(environmentId: string, snapshotId: string): Promise<Environment> {
    return authedRequest<Environment>(
        `/api/v1/environments/${environmentId}/snapshots/${snapshotId}/restore`,
        { method: "POST" },
    );
}

export async function deleteSnapshot(environmentId: string, snapshotId: string): Promise<void> {
    return authedRequest<void>(`/api/v1/environments/${environmentId}/snapshots/${snapshotId}`, {
        method: "DELETE",
    });
}

// --- Sharing ---

export async function shareEnvironment(environmentId: string, email: string): Promise<EnvironmentShare> {
    return authedRequest<EnvironmentShare>(`/api/v1/environments/${environmentId}/shares`, {
        method: "POST",
        body: JSON.stringify({ email }),
    });
}

export async function getShares(environmentId: string): Promise<EnvironmentShare[]> {
    const body = await authedRequest<{ shares: EnvironmentShare[] }>(
        `/api/v1/environments/${environmentId}/shares`,
        { method: "GET" },
    );
    return body.shares ?? [];
}

export async function unshareEnvironment(environmentId: string, email: string): Promise<void> {
    return authedRequest<void>(
        `/api/v1/environments/${environmentId}/shares/${encodeURIComponent(email)}`,
        { method: "DELETE" },
    );
}

// --- Browser IDE ---

export async function startIDE(environmentId: string): Promise<IDEStatus> {
    return authedRequest<IDEStatus>(`/api/v1/environments/${environmentId}/ide/start`, { method: "POST" });
}

export async function stopIDE(environmentId: string): Promise<void> {
    return authedRequest<void>(`/api/v1/environments/${environmentId}/ide/stop`, { method: "POST" });
}

export async function getIDEStatus(environmentId: string): Promise<IDEStatus> {
    return authedRequest<IDEStatus>(`/api/v1/environments/${environmentId}/ide`, { method: "GET" });
}

// --- Usage & billing ---

export async function getUsage(): Promise<UsageSummary> {
    return authedRequest<UsageSummary>("/api/v1/usage", { method: "GET" });
}

export async function getBillingSummary(): Promise<BillingSummary> {
    return authedRequest<BillingSummary>("/api/v1/billing/summary", { method: "GET" });
}

export async function getBudget(): Promise<UserSettings> {
    return authedRequest<UserSettings>("/api/v1/billing/budget", { method: "GET" });
}

export async function updateBudget(monthlyBudgetUSD: number, alertsEnabled: boolean): Promise<UserSettings> {
    return authedRequest<UserSettings>("/api/v1/billing/budget", {
        method: "PUT",
        body: JSON.stringify({
            monthly_budget_usd: monthlyBudgetUSD,
            budget_alerts_enabled: alertsEnabled,
        }),
    });
}
