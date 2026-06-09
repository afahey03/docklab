import { getToken } from "./auth";

const API_BASE_URL =
    import.meta.env.VITE_API_BASE_URL?.toString() ?? "http://localhost:8080";

type ErrorBody = {
    code?: string;
    error?: string;
};

type AuthResponse = {
    token: string;
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

export type RemoteHealthStatus = {
    runtime_target: string;
    public_ip: string;
    ssh_reachable: boolean;
    docker_available: boolean;
    workspace_ready?: boolean;
    error?: string;
};

export type CreateEnvironmentRequest = {
    name?: string;
    image: string;
    target?: "local" | "cloud";
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
};

function withAuthHeaders(headers?: HeadersInit): HeadersInit {
    const token = getToken();
    if (!token) {
        throw new Error("missing auth token");
    }

    return {
        Authorization: `Bearer ${token}`,
        ...(headers ?? {}),
    };
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
    return request<MeResponse>("/api/v1/auth/me", {
        method: "GET",
        headers: withAuthHeaders(),
    });
}

export async function getEnvironments(): Promise<Environment[]> {
    const body = await request<EnvironmentsResponse>("/api/v1/environments", {
        method: "GET",
        headers: withAuthHeaders(),
    });

    return body.environments;
}

export async function createEnvironment(payload: CreateEnvironmentRequest): Promise<CreateEnvironmentResponse> {
    return request<CreateEnvironmentResponse>("/api/v1/environments", {
        method: "POST",
        headers: withAuthHeaders(),
        body: JSON.stringify({
            name: payload.name?.trim() ?? "",
            image: payload.image.trim(),
            target: payload.target ?? "local",
            provision: payload.provision,
        }),
    });
}

export async function startEnvironment(id: string): Promise<Environment> {
    return request<Environment>(`/api/v1/environments/${id}/start`, {
        method: "POST",
        headers: withAuthHeaders(),
    });
}

export async function stopEnvironment(id: string): Promise<Environment> {
    return request<Environment>(`/api/v1/environments/${id}/stop`, {
        method: "POST",
        headers: withAuthHeaders(),
    });
}

export async function deleteEnvironment(id: string): Promise<Operation> {
    return request<Operation>(`/api/v1/environments/${id}`, {
        method: "DELETE",
        headers: withAuthHeaders(),
    });
}

export async function provisionEnvironment(id: string, payload: ProvisionRequest): Promise<Operation> {
    return request<Operation>(`/api/v1/environments/${id}/provision`, {
        method: "POST",
        headers: withAuthHeaders(),
        body: JSON.stringify(payload),
    });
}

export async function destroyCloudEnvironment(id: string): Promise<Operation> {
    return request<Operation>(`/api/v1/environments/${id}/destroy-cloud`, {
        method: "POST",
        headers: withAuthHeaders(),
    });
}

export async function retryRemoteBootstrap(id: string): Promise<Operation> {
    return request<Operation>(`/api/v1/environments/${id}/retry-bootstrap`, {
        method: "POST",
        headers: withAuthHeaders(),
    });
}

export async function getRemoteHealth(id: string): Promise<RemoteHealthStatus> {
    return request<RemoteHealthStatus>(`/api/v1/environments/${id}/remote-health`, {
        method: "GET",
        headers: withAuthHeaders(),
    });
}

export async function getOperation(id: string): Promise<Operation> {
    return request<Operation>(`/api/v1/operations/${id}`, {
        method: "GET",
        headers: withAuthHeaders(),
    });
}
