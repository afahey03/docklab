const TOKEN_STORAGE_KEY = "docklab.auth.token";
const REFRESH_TOKEN_STORAGE_KEY = "docklab.auth.refresh_token";

export function getToken(): string | null {
    return localStorage.getItem(TOKEN_STORAGE_KEY);
}

export function getRefreshToken(): string | null {
    return localStorage.getItem(REFRESH_TOKEN_STORAGE_KEY);
}

export function setToken(token: string): void {
    localStorage.setItem(TOKEN_STORAGE_KEY, token);
}

export function setTokens(token: string, refreshToken?: string): void {
    localStorage.setItem(TOKEN_STORAGE_KEY, token);
    if (refreshToken) {
        localStorage.setItem(REFRESH_TOKEN_STORAGE_KEY, refreshToken);
    }
}

export function clearToken(): void {
    localStorage.removeItem(TOKEN_STORAGE_KEY);
    localStorage.removeItem(REFRESH_TOKEN_STORAGE_KEY);
}

export function isAuthenticated(): boolean {
    return Boolean(getToken());
}
