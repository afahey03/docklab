import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import {
    createEnvironment,
    destroyCloudEnvironment,
    deleteEnvironment,
    retryRemoteBootstrap,
    getRemoteHealth,
    getEnvironments,
    getMe,
    getOperation,
    provisionEnvironment,
    startEnvironment,
    stopEnvironment,
    type Environment,
    type Operation,
    type RemoteHealthStatus,
} from "../lib/api";
import { clearToken, getToken } from "../lib/auth";
import { getEnvironmentCapabilities } from "../lib/environmentCapabilities";

type EnvironmentAction = "start" | "stop" | "delete" | "provision" | "destroy_cloud" | "retry_bootstrap";
type ConfirmAction = "delete_environment" | "destroy_cloud";
type DashboardView = "environments" | "usage" | "settings";

type ConfirmDialogState = {
    open: boolean;
    environmentId: string;
    title: string;
    description: string;
    confirmLabel: string;
    action: ConfirmAction;
    destructive?: boolean;
};

const OPERATION_POLL_INTERVAL_MS = 2000;
const OPERATION_TIMEOUT_MS = 20 * 60 * 1000;
const RUNNING_ENVIRONMENT_REFRESH_INTERVAL_MS = 5000;
const IDLE_ENVIRONMENT_REFRESH_INTERVAL_MS = 30000;

const INSTANCE_HOURLY_RATE_USD: Record<string, number> = {
    "t3.nano": 0.0052,
    "t3.micro": 0.0104,
    "t3.small": 0.0208,
    "t3.medium": 0.0416,
    "t3.large": 0.0832,
    "t3.xlarge": 0.1664,
    "t3.2xlarge": 0.3328,
};

const currencyFormatter = new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
});

type EnvironmentUsageSummary = {
    isCloudActive: boolean;
    runtimeHours: number | null;
    formattedRuntime: string;
    hourlyRate: number | null;
    estimatedSpend: number | null;
    estimatedMonthly: number | null;
};

function getCloudHourlyRate(instanceType: string): number | null {
    if (!instanceType) {
        return null;
    }

    return INSTANCE_HOURLY_RATE_USD[instanceType.toLowerCase()] ?? null;
}

function formatCurrency(value: number | null): string {
    if (value === null) {
        return "N/A";
    }

    return currencyFormatter.format(value);
}

function formatRuntimeHours(hours: number | null): string {
    if (hours === null || !Number.isFinite(hours) || hours < 0) {
        return "N/A";
    }

    const totalMinutes = Math.max(0, Math.floor(hours * 60));
    const days = Math.floor(totalMinutes / (24 * 60));
    const remainingMinutesAfterDays = totalMinutes % (24 * 60);
    const wholeHours = Math.floor(remainingMinutesAfterDays / 60);
    const minutes = remainingMinutesAfterDays % 60;

    if (days > 0) {
        return `${days}d ${wholeHours}h`;
    }
    if (wholeHours > 0) {
        return `${wholeHours}h ${minutes}m`;
    }
    return `${minutes}m`;
}

function getEnvironmentUsageSummary(env: Environment): EnvironmentUsageSummary {
    const isCloudActive = Boolean(env.instance_id || env.terraform_dir || env.cloud_status === "provisioned");
    const hourlyRate = getCloudHourlyRate(env.cloud_instance_type);

    if (!isCloudActive || !env.cloud_provisioned_at) {
        return {
            isCloudActive,
            runtimeHours: null,
            formattedRuntime: "N/A",
            hourlyRate,
            estimatedSpend: null,
            estimatedMonthly: hourlyRate === null ? null : hourlyRate * 24 * 30,
        };
    }

    const provisionedAt = new Date(env.cloud_provisioned_at);
    const runtimeHours = Number.isNaN(provisionedAt.getTime())
        ? null
        : Math.max(0, (Date.now() - provisionedAt.getTime()) / (1000 * 60 * 60));

    return {
        isCloudActive,
        runtimeHours,
        formattedRuntime: formatRuntimeHours(runtimeHours),
        hourlyRate,
        estimatedSpend: runtimeHours === null || hourlyRate === null ? null : runtimeHours * hourlyRate,
        estimatedMonthly: hourlyRate === null ? null : hourlyRate * 24 * 30,
    };
}

export function DashboardPage() {
    const navigate = useNavigate();
    const [activeView, setActiveView] = useState<DashboardView>("environments");
    const [email, setEmail] = useState("");
    const [environments, setEnvironments] = useState<Environment[]>([]);
    const [name, setName] = useState("");
    const [image, setImage] = useState("alpine:3.20");
    const [awsRegion, setAWSRegion] = useState("us-east-1");
    const [instanceType, setInstanceType] = useState("t3.micro");
    const [amiID, setAMIID] = useState("ami-0c2b8ca1dad447f8a");
    const [keyName, setKeyName] = useState("");
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const [isLoadingEnvironments, setIsLoadingEnvironments] = useState(true);
    const [isCreating, setIsCreating] = useState(false);
    const [pendingActions, setPendingActions] = useState<Record<string, EnvironmentAction | undefined>>({});
    const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState>({
        open: false,
        environmentId: "",
        title: "",
        description: "",
        confirmLabel: "Confirm",
        action: "delete_environment",
    });
    const [activeTerminalEnvironmentId, setActiveTerminalEnvironmentId] = useState("");
    const [terminalConnected, setTerminalConnected] = useState(false);
    const [remoteHealthByEnvironment, setRemoteHealthByEnvironment] = useState<Record<string, RemoteHealthStatus | undefined>>({});
    const terminalContainerRef = useRef<HTMLDivElement | null>(null);
    const wsRef = useRef<WebSocket | null>(null);
    const xtermRef = useRef<Terminal | null>(null);
    const fitAddonRef = useRef<FitAddon | null>(null);
    const terminalReconnectTimerRef = useRef<number | null>(null);
    const manualTerminalCloseRef = useRef(false);
    const reconnectAttemptsRef = useRef(0);
    const environmentUsage = environments.map((environment) => ({
        environment,
        usage: getEnvironmentUsageSummary(environment),
    }));
    const activeCloudUsage = environmentUsage.filter((entry) => entry.usage.isCloudActive);
    const activeCloudEnvironmentCount = environmentUsage.filter((entry) => entry.usage.isCloudActive).length;
    const totalEstimatedSpend = environmentUsage.reduce((total, entry) => total + (entry.usage.estimatedSpend ?? 0), 0);
    const totalEstimatedMonthly = environmentUsage.reduce((total, entry) => total + (entry.usage.estimatedMonthly ?? 0), 0);

    useEffect(() => {
        if (!terminalContainerRef.current) {
            return;
        }

        const fitAddon = new FitAddon();
        const terminal = new Terminal({
            cursorBlink: true,
            convertEol: true,
            scrollback: 2000,
            fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace",
            fontSize: 13,
            rows: 24,
            cols: 80,
            theme: {
                background: "#020617",
                foreground: "#e2e8f0",
                cursor: "#22d3ee",
            },
        });
        terminal.loadAddon(fitAddon);
        terminal.open(terminalContainerRef.current);
        fitAddon.fit();
        terminal.writeln("[docklab] terminal ready");

        terminal.attachCustomKeyEventHandler((event) => {
            if (event.ctrlKey && event.shiftKey && event.code === "KeyC") {
                const selection = terminal.getSelection();
                if (selection) {
                    void navigator.clipboard.writeText(selection);
                }
                return false;
            }

            if (event.ctrlKey && event.shiftKey && event.code === "KeyV") {
                void navigator.clipboard.readText().then((text) => {
                    const socket = wsRef.current;
                    if (!socket || socket.readyState !== WebSocket.OPEN || text.length === 0) {
                        return;
                    }
                    socket.send(JSON.stringify({ type: "input", data: text }));
                });
                return false;
            }

            return true;
        });

        const inputDisposable = terminal.onData((data) => {
            const socket = wsRef.current;
            if (!socket || socket.readyState !== WebSocket.OPEN) {
                return;
            }
            socket.send(JSON.stringify({ type: "input", data }));
        });

        const resizeDisposable = terminal.onResize(({ cols, rows }) => {
            const socket = wsRef.current;
            if (!socket || socket.readyState !== WebSocket.OPEN) {
                return;
            }
            socket.send(JSON.stringify({ type: "resize", cols, rows }));
        });

        xtermRef.current = terminal;
        fitAddonRef.current = fitAddon;

        const handleWindowResize = () => {
            fitAddon.fit();
            const socket = wsRef.current;
            if (socket && socket.readyState === WebSocket.OPEN) {
                socket.send(JSON.stringify({ type: "resize", cols: terminal.cols, rows: terminal.rows }));
            }
        };
        window.addEventListener("resize", handleWindowResize);

        return () => {
            if (terminalReconnectTimerRef.current !== null) {
                window.clearTimeout(terminalReconnectTimerRef.current);
                terminalReconnectTimerRef.current = null;
            }
            window.removeEventListener("resize", handleWindowResize);
            inputDisposable.dispose();
            resizeDisposable.dispose();
            terminal.dispose();
            xtermRef.current = null;
            fitAddonRef.current = null;
        };
    }, []);

    useEffect(() => {
        async function bootstrapDashboard() {
            try {
                const [user, envs] = await Promise.all([getMe(), getEnvironments()]);
                setEmail(user.email);
                setEnvironments(envs);
            } catch {
                clearToken();
                navigate("/login", { replace: true });
            } finally {
                setIsLoadingEnvironments(false);
            }
        }

        void bootstrapDashboard();
    }, [navigate]);

    useEffect(() => {
        const refreshInterval = environments.some((environment) => environment.status === "running")
            ? RUNNING_ENVIRONMENT_REFRESH_INTERVAL_MS
            : IDLE_ENVIRONMENT_REFRESH_INTERVAL_MS;

        const intervalID = window.setInterval(() => {
            void refreshEnvironments().catch(() => {
                // Ignore transient refresh failures and preserve the last known dashboard state.
            });
        }, refreshInterval);

        return () => {
            window.clearInterval(intervalID);
        };
    }, [environments]);

    useEffect(() => {
        return () => {
            if (wsRef.current) {
                wsRef.current.close();
                wsRef.current = null;
            }
            if (terminalReconnectTimerRef.current !== null) {
                window.clearTimeout(terminalReconnectTimerRef.current);
                terminalReconnectTimerRef.current = null;
            }
        };
    }, []);

    function handleSignOut() {
        closeTerminal();
        clearToken();
        navigate("/login", { replace: true });
    }

    function closeTerminal() {
        manualTerminalCloseRef.current = Boolean(wsRef.current);
        if (wsRef.current) {
            wsRef.current.close();
            wsRef.current = null;
        }
        if (terminalReconnectTimerRef.current !== null) {
            window.clearTimeout(terminalReconnectTimerRef.current);
            terminalReconnectTimerRef.current = null;
        }

        reconnectAttemptsRef.current = 0;
        setTerminalConnected(false);
        setActiveTerminalEnvironmentId("");
    }

    function connectTerminal(environmentId: string) {
        const terminal = xtermRef.current;
        if (!terminal) {
            setError("terminal is not initialized");
            return;
        }

        const token = getToken();
        if (!token) {
            setError("missing auth token");
            return;
        }

        const apiBaseUrl = import.meta.env.VITE_API_BASE_URL?.toString() ?? "http://localhost:8080";
        const wsBase = apiBaseUrl.replace("http://", "ws://").replace("https://", "wss://");
        const wsUrl = `${wsBase}/api/v1/environments/${environmentId}/terminal/ws?token=${encodeURIComponent(token)}`;

        const socket = new WebSocket(wsUrl);
        socket.onopen = () => {
            reconnectAttemptsRef.current = 0;
            setTerminalConnected(true);
            terminal.writeln("[docklab] terminal connected");
            fitAddonRef.current?.fit();
            socket.send(JSON.stringify({ type: "resize", cols: terminal.cols, rows: terminal.rows }));
        };
        socket.onmessage = (event) => {
            terminal.write(String(event.data));
        };
        socket.onerror = () => {
            terminal.writeln("\r\n[docklab] terminal connection error");
        };
        socket.onclose = () => {
            setTerminalConnected(false);
            wsRef.current = null;

            if (manualTerminalCloseRef.current) {
                manualTerminalCloseRef.current = false;
                terminal.writeln("\r\n[docklab] terminal disconnected");
                setActiveTerminalEnvironmentId("");
                return;
            }

            reconnectAttemptsRef.current += 1;
            if (reconnectAttemptsRef.current > 5) {
                terminal.writeln("\r\n[docklab] terminal disconnected; reconnect limit reached");
                return;
            }

            terminal.writeln("\r\n[docklab] terminal disconnected; reconnecting...");
            if (terminalReconnectTimerRef.current !== null) {
                window.clearTimeout(terminalReconnectTimerRef.current);
            }
            terminalReconnectTimerRef.current = window.setTimeout(() => {
                connectTerminal(environmentId);
            }, 1500);
        };

        wsRef.current = socket;
    }

    function openTerminal(environmentId: string) {
        if (activeTerminalEnvironmentId === environmentId) {
            return;
        }

        closeTerminal();
        setError("");
        const terminal = xtermRef.current;
        if (!terminal) {
            setError("terminal is not initialized");
            return;
        }

        terminal.clear();
        terminal.writeln("[docklab] connecting to terminal...");
        setActiveTerminalEnvironmentId(environmentId);
        connectTerminal(environmentId);
    }

    function reconnectTerminal() {
        if (!activeTerminalEnvironmentId || terminalConnected) {
            return;
        }

        const terminal = xtermRef.current;
        if (terminal) {
            terminal.writeln("[docklab] reconnect requested");
        }
        connectTerminal(activeTerminalEnvironmentId);
    }

    async function refreshEnvironments() {
        const envs = await getEnvironments();
        setEnvironments(envs);
    }

    function replaceEnvironment(updated: Environment) {
        setEnvironments((previous) => previous.map((item) => (item.id === updated.id ? updated : item)));
    }

    function setEnvironmentPendingAction(id: string, action?: EnvironmentAction) {
        setPendingActions((previous) => {
            const next = { ...previous };
            if (action) {
                next[id] = action;
            } else {
                delete next[id];
            }
            return next;
        });
    }

    function sleep(ms: number) {
        return new Promise((resolve) => {
            window.setTimeout(resolve, ms);
        });
    }

    async function waitForOperation(operation: Operation, successMessage: string) {
        const started = Date.now();

        while (Date.now() - started < OPERATION_TIMEOUT_MS) {
            const latest = await getOperation(operation.id);
            if (latest.status === "succeeded") {
                await refreshEnvironments();
                setNotice(successMessage);
                return;
            }
            if (latest.status === "failed") {
                await refreshEnvironments();
                throw new Error(latest.error || "operation failed");
            }

            await sleep(OPERATION_POLL_INTERVAL_MS);
        }

        throw new Error("operation timed out while waiting for completion");
    }

    function isEnvironmentPending(id: string) {
        return Boolean(pendingActions[id]);
    }

    function isEnvironmentActionPending(id: string, action: EnvironmentAction) {
        return pendingActions[id] === action;
    }

    async function handleCreateEnvironment() {
        setError("");
        setNotice("");
        const trimmedName = name.trim();
        const trimmedImage = image.trim();

        if (!trimmedImage) {
            setError("docker image is required");
            return;
        }

        setIsCreating(true);
        try {
            const created = await createEnvironment(trimmedName, trimmedImage);
            setEnvironments((previous) => [created, ...previous]);
            setName("");
            setNotice("environment created");
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to create environment");
        } finally {
            setIsCreating(false);
        }
    }

    async function handleStartEnvironment(id: string) {
        setError("");
        setNotice("");
        setEnvironmentPendingAction(id, "start");
        try {
            const updated = await startEnvironment(id);
            replaceEnvironment(updated);
            setNotice("environment started");
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to start environment");
        } finally {
            setEnvironmentPendingAction(id);
        }
    }

    async function handleStopEnvironment(id: string) {
        setError("");
        setNotice("");
        setEnvironmentPendingAction(id, "stop");
        try {
            const updated = await stopEnvironment(id);
            replaceEnvironment(updated);
            setNotice("environment stopped");
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to stop environment");
        } finally {
            setEnvironmentPendingAction(id);
        }
    }

    function promptDeleteEnvironment(id: string) {
        const env = environments.find((item) => item.id === id);
        if (!env) {
            return;
        }

        const hasCloudResources = Boolean(env.instance_id || env.terraform_dir || env.cloud_status === "provisioned");
        setConfirmDialog({
            open: true,
            environmentId: id,
            title: hasCloudResources ? "Delete Environment And Cloud Resources" : "Delete Environment",
            description: hasCloudResources
                ? "This will terminate provisioned EC2 infrastructure and remove the environment from DockLab."
                : "This will remove the environment from DockLab.",
            confirmLabel: "Delete",
            action: "delete_environment",
            destructive: true,
        });
    }

    function promptDestroyCloudEnvironment(id: string) {
        const env = environments.find((item) => item.id === id);
        if (!env) {
            return;
        }

        setConfirmDialog({
            open: true,
            environmentId: id,
            title: "Terminate Cloud Resources",
            description: "This will terminate the provisioned EC2 resources and keep the environment in DockLab.",
            confirmLabel: "Terminate EC2",
            action: "destroy_cloud",
            destructive: true,
        });
    }

    function closeConfirmDialog() {
        setConfirmDialog((previous) => ({ ...previous, open: false }));
    }

    async function runDeleteEnvironment(id: string) {
        setError("");
        setNotice("");
        setEnvironmentPendingAction(id, "delete");
        try {
            const operation = await deleteEnvironment(id);
            await waitForOperation(operation, "environment deleted");
            if (activeTerminalEnvironmentId === id) {
                closeTerminal();
            }
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to delete environment");
        } finally {
            setEnvironmentPendingAction(id);
        }
    }

    async function runDestroyCloudEnvironment(id: string) {
        setError("");
        setNotice("");
        setEnvironmentPendingAction(id, "destroy_cloud");
        try {
            const operation = await destroyCloudEnvironment(id);
            await waitForOperation(operation, "cloud resources terminated");
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to terminate cloud resources");
        } finally {
            setEnvironmentPendingAction(id);
        }
    }

    async function handleConfirmAction() {
        const { environmentId, action } = confirmDialog;
        closeConfirmDialog();
        if (!environmentId) {
            return;
        }

        if (action === "destroy_cloud") {
            await runDestroyCloudEnvironment(environmentId);
            return;
        }

        await runDeleteEnvironment(environmentId);
    }

    async function handleProvisionEnvironment(id: string) {
        setError("");
        setNotice("");

        if (!awsRegion.trim()) {
            setError("aws region is required");
            return;
        }
        if (!instanceType.trim()) {
            setError("instance type is required");
            return;
        }
        if (!amiID.trim()) {
            setError("AMI ID is required");
            return;
        }
        if (!keyName.trim()) {
            setError("EC2 key pair name is required for remote SSH access");
            return;
        }

        const normalizedKeyName = keyName.trim().replace(/\.pem$/i, "");
        if (normalizedKeyName !== keyName.trim()) {
            setNotice("Using EC2 key pair name without .pem extension.");
        }

        setEnvironmentPendingAction(id, "provision");
        try {
            const operation = await provisionEnvironment(id, {
                region: awsRegion.trim(),
                instance_type: instanceType.trim(),
                ami: amiID.trim(),
                key_name: normalizedKeyName,
            });
            await waitForOperation(operation, "provisioning finished");
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to provision environment");
        } finally {
            setEnvironmentPendingAction(id);
        }
    }

    async function handleRetryRemoteBootstrap(id: string) {
        setError("");
        setNotice("");
        setEnvironmentPendingAction(id, "retry_bootstrap");
        try {
            const operation = await retryRemoteBootstrap(id);
            await waitForOperation(operation, "remote workspace setup finished");
            await refreshEnvironments();
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to complete remote bootstrap");
        } finally {
            setEnvironmentPendingAction(id);
        }
    }

    function cloudStatusBadgeClass(cloudStatus: string): string {
        switch (cloudStatus) {
            case "provisioned":
                return "border-emerald-700 text-emerald-300";
            case "provisioning":
            case "deprovisioning":
                return "border-amber-700 text-amber-300";
            case "provision_failed":
                return "border-rose-700 text-rose-300";
            default:
                return "border-slate-700 text-slate-300";
        }
    }

    function workspaceStatusBadgeClass(status: string): string {
        return status === "running" ? "border-cyan-700 text-cyan-300" : "border-slate-700 text-slate-300";
    }

    async function handleCheckRemoteHealth(id: string) {
        setError("");
        try {
            const health = await getRemoteHealth(id);
            setRemoteHealthByEnvironment((current) => ({ ...current, [id]: health }));
        } catch (requestError) {
            setError(requestError instanceof Error ? requestError.message : "failed to check remote health");
        }
    }

    return (
        <>
            <main className="min-h-screen bg-slate-950 text-slate-100">
                <div className="mx-auto grid max-w-6xl gap-6 p-6 md:grid-cols-[240px_1fr]">
                    <aside className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                        <h2 className="text-lg font-semibold">DockLab</h2>
                        <nav className="mt-4 space-y-2 text-sm text-slate-300">
                            <button
                                className={`w-full rounded-md px-3 py-2 text-left ${activeView === "environments" ? "bg-slate-800 text-slate-100" : "text-slate-300 hover:bg-slate-800/60"}`}
                                type="button"
                                onClick={() => setActiveView("environments")}
                            >
                                Environments
                            </button>
                            <button
                                className={`w-full rounded-md px-3 py-2 text-left ${activeView === "usage" ? "bg-slate-800 text-slate-100" : "text-slate-300 hover:bg-slate-800/60"}`}
                                type="button"
                                onClick={() => setActiveView("usage")}
                            >
                                Usage & Cost
                            </button>
                            <button
                                className={`w-full rounded-md px-3 py-2 text-left ${activeView === "settings" ? "bg-slate-800 text-slate-100" : "text-slate-300 hover:bg-slate-800/60"}`}
                                type="button"
                                onClick={() => setActiveView("settings")}
                            >
                                Settings
                            </button>
                        </nav>
                    </aside>

                    <section className="space-y-4">
                        <header className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                            <h1 className="text-xl font-semibold">
                                {activeView === "environments"
                                    ? "Environments"
                                    : activeView === "usage"
                                        ? "Usage & Cost"
                                        : "Settings"}
                            </h1>
                            <p className="mt-1 text-sm text-slate-400">
                                {activeView === "environments"
                                    ? "Launch and manage remote development environments."
                                    : activeView === "usage"
                                        ? "Review estimated EC2 runtime and cloud spend for your provisioned environments."
                                        : "Adjust shared defaults for provisioning and environment management."}
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

                        {notice ? (
                            <article className="rounded-xl border border-emerald-800 bg-emerald-950/30 p-4 text-sm text-emerald-300">
                                {notice}
                            </article>
                        ) : null}
                        {error ? (
                            <article className="rounded-xl border border-rose-800 bg-rose-950/30 p-4 text-sm text-rose-300">
                                {error}
                            </article>
                        ) : null}

                        {activeView === "environments" ? (
                            <>
                                <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                                    <h3 className="font-medium">Create environment</h3>
                                    <p className="mt-1 text-sm text-slate-400">Launch a local Docker workspace for your user.</p>

                                    <div className="mt-4 grid gap-3 md:grid-cols-[1fr_1fr_auto]">
                                        <input
                                            className="rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500 focus:ring"
                                            placeholder="Environment name (optional)"
                                            value={name}
                                            onChange={(event) => setName(event.target.value)}
                                            maxLength={64}
                                        />
                                        <input
                                            className="rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500 focus:ring"
                                            placeholder="Docker image"
                                            value={image}
                                            onChange={(event) => setImage(event.target.value)}
                                            maxLength={128}
                                        />
                                        <button
                                            className="rounded-md bg-cyan-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-cyan-400 disabled:opacity-60"
                                            type="button"
                                            disabled={isCreating}
                                            onClick={handleCreateEnvironment}
                                        >
                                            {isCreating ? "Working..." : "Create"}
                                        </button>
                                    </div>
                                </article>

                                <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                                    <h3 className="font-medium">Your environments</h3>
                                    {isLoadingEnvironments ? (
                                        <p className="mt-1 text-sm text-slate-400">Loading environments...</p>
                                    ) : environments.length === 0 ? (
                                        <p className="mt-1 text-sm text-slate-400">No environments yet.</p>
                                    ) : (
                                        <div className="mt-3 space-y-3">
                                            {environmentUsage.map(({ environment: env }) => {
                                                const capabilities = getEnvironmentCapabilities(env, isEnvironmentPending(env.id));
                                                const remoteHealth = remoteHealthByEnvironment[env.id];

                                                return (
                                                <div key={env.id} className="rounded-md border border-slate-800 bg-slate-950 p-3">
                                                    <div className="flex flex-wrap items-center justify-between gap-2">
                                                        <div>
                                                            <p className="font-medium text-slate-100">{env.name}</p>
                                                            <p className="text-xs text-slate-400">{env.image}</p>
                                                            <p className="text-xs text-slate-500">
                                                                Runtime: {env.runtime_target || "local"}
                                                                {" | "}
                                                                Cloud: {env.cloud_status || "not_provisioned"}
                                                                {env.public_ip ? ` | IP: ${env.public_ip}` : ""}
                                                            </p>
                                                            {env.cloud_instance_type ? (
                                                                <p className="text-xs text-slate-500">
                                                                    Type: {env.cloud_instance_type}
                                                                    {env.cloud_region ? ` | Region: ${env.cloud_region}` : ""}
                                                                </p>
                                                            ) : null}
                                                            {env.cloud_error ? (
                                                                <p className="text-xs text-rose-400">{env.cloud_error}</p>
                                                            ) : null}
                                                            {remoteHealth ? (
                                                                <p className="text-xs text-slate-500">
                                                                    Remote health: SSH {remoteHealth.ssh_reachable ? "ok" : "down"}
                                                                    {" | "}
                                                                    Docker {remoteHealth.docker_available ? "ok" : "down"}
                                                                    {" | "}
                                                                    Workspace {remoteHealth.workspace_ready ? "ok" : "down"}
                                                                    {remoteHealth.error ? ` — ${remoteHealth.error}` : ""}
                                                                </p>
                                                            ) : null}
                                                            {capabilities.showRemoteBootstrapHint ? (
                                                                <p className="text-xs text-amber-400">
                                                                    EC2 is provisioned but the remote workspace is not attached yet.
                                                                </p>
                                                            ) : null}
                                                        </div>
                                                        <div className="flex flex-wrap gap-2">
                                                            <span className={`rounded-full border px-2 py-1 text-xs ${workspaceStatusBadgeClass(capabilities.workspaceStatusLabel)}`}>
                                                                workspace: {capabilities.workspaceStatusLabel}
                                                            </span>
                                                            {env.instance_id ? (
                                                                <span className={`rounded-full border px-2 py-1 text-xs ${cloudStatusBadgeClass(capabilities.cloudStatusLabel)}`}>
                                                                    cloud: {capabilities.cloudStatusLabel}
                                                                </span>
                                                            ) : null}
                                                        </div>
                                                    </div>

                                                    <div className="mt-3 flex flex-wrap gap-2">
                                                        <button
                                                            className="rounded-md border border-emerald-700 px-3 py-1 text-xs text-emerald-300 hover:bg-emerald-950 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                                                            type="button"
                                                            disabled={!capabilities.canStart}
                                                            onClick={() => handleStartEnvironment(env.id)}
                                                        >
                                                            {isEnvironmentActionPending(env.id, "start") ? "Starting..." : "Start"}
                                                        </button>
                                                        <button
                                                            className="rounded-md border border-amber-700 px-3 py-1 text-xs text-amber-300 hover:bg-amber-950 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                                                            type="button"
                                                            disabled={!capabilities.canStop}
                                                            onClick={() => handleStopEnvironment(env.id)}
                                                        >
                                                            {isEnvironmentActionPending(env.id, "stop") ? "Stopping..." : "Stop"}
                                                        </button>
                                                        <button
                                                            className="rounded-md border border-rose-700 px-3 py-1 text-xs text-rose-300 hover:bg-rose-950 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                                                            type="button"
                                                            disabled={!capabilities.canDelete}
                                                            onClick={() => promptDeleteEnvironment(env.id)}
                                                        >
                                                            {isEnvironmentActionPending(env.id, "delete") ? "Deleting..." : "Delete"}
                                                        </button>
                                                        <button
                                                            className="rounded-md border border-fuchsia-700 px-3 py-1 text-xs text-fuchsia-300 hover:bg-fuchsia-950 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                                                            type="button"
                                                            disabled={!capabilities.canTerminateEC2}
                                                            onClick={() => promptDestroyCloudEnvironment(env.id)}
                                                        >
                                                            {isEnvironmentActionPending(env.id, "destroy_cloud") ? "Terminating..." : "Terminate EC2"}
                                                        </button>
                                                        <button
                                                            className="rounded-md border border-cyan-700 px-3 py-1 text-xs text-cyan-300 hover:bg-cyan-950 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                                                            type="button"
                                                            disabled={!capabilities.canOpenTerminal || activeTerminalEnvironmentId === env.id}
                                                            onClick={() => openTerminal(env.id)}
                                                        >
                                                            {activeTerminalEnvironmentId === env.id ? "Terminal open" : "Terminal"}
                                                        </button>
                                                        <button
                                                            className="rounded-md border border-indigo-700 px-3 py-1 text-xs text-indigo-300 hover:bg-indigo-950 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                                                            type="button"
                                                            disabled={!capabilities.canProvision}
                                                            onClick={() => handleProvisionEnvironment(env.id)}
                                                        >
                                                            {isEnvironmentActionPending(env.id, "provision")
                                                                ? "Provisioning..."
                                                                : capabilities.provisionLabel}
                                                        </button>
                                                        {capabilities.canRepairRemoteWorkspace ? (
                                                            <button
                                                                className="rounded-md border border-amber-600 px-3 py-1 text-xs text-amber-300 hover:bg-amber-950 disabled:cursor-not-allowed disabled:opacity-50"
                                                                type="button"
                                                                disabled={isEnvironmentPending(env.id)}
                                                                onClick={() => void handleRetryRemoteBootstrap(env.id)}
                                                            >
                                                                {isEnvironmentActionPending(env.id, "retry_bootstrap")
                                                                    ? "Setting up remote..."
                                                                    : capabilities.repairRemoteLabel}
                                                            </button>
                                                        ) : null}
                                                        {capabilities.canCheckRemoteHealth ? (
                                                            <button
                                                                className="rounded-md border border-slate-600 px-3 py-1 text-xs text-slate-300 hover:bg-slate-900 disabled:cursor-not-allowed disabled:opacity-50"
                                                                type="button"
                                                                disabled={isEnvironmentPending(env.id)}
                                                                onClick={() => void handleCheckRemoteHealth(env.id)}
                                                            >
                                                                Check remote health
                                                            </button>
                                                        ) : null}
                                                    </div>
                                                </div>
                                                );
                                            })}
                                        </div>
                                    )}
                                </article>

                                <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                                    <div className="flex items-center justify-between">
                                        <h3 className="font-medium">Browser terminal</h3>
                                        <div className="flex gap-2">
                                            <button
                                                className="rounded-md border border-cyan-700 px-3 py-1 text-xs text-cyan-300 hover:bg-cyan-950 disabled:cursor-not-allowed disabled:opacity-50"
                                                type="button"
                                                disabled={!activeTerminalEnvironmentId || terminalConnected}
                                                onClick={reconnectTerminal}
                                            >
                                                Reconnect
                                            </button>
                                            <button
                                                className="rounded-md border border-slate-700 px-3 py-1 text-xs text-slate-300 hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                                                type="button"
                                                disabled={!activeTerminalEnvironmentId}
                                                onClick={closeTerminal}
                                            >
                                                Close session
                                            </button>
                                        </div>
                                    </div>

                                    {!activeTerminalEnvironmentId ? (
                                        <p className="mt-2 text-sm text-slate-400">
                                            Select Terminal on a running environment to start a shell session.
                                        </p>
                                    ) : (
                                        <div className="mt-2 flex items-center justify-between">
                                            <p className="text-xs text-slate-400">
                                                Active environment: {activeTerminalEnvironmentId}
                                            </p>
                                            <p className="text-xs text-slate-500">
                                                {terminalConnected ? "Connected" : "Disconnected"} | Copy: Ctrl+Shift+C | Paste: Ctrl+Shift+V
                                            </p>
                                        </div>
                                    )}

                                    <div className="mt-3 rounded-md border border-slate-800 bg-slate-950 p-2">
                                        <div className="h-72" ref={terminalContainerRef} />
                                    </div>
                                </article>
                            </>
                        ) : null}

                        {activeView === "usage" ? (
                            <>
                                <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                                    <div className="flex items-center justify-between gap-3">
                                        <div>
                                            <h3 className="font-medium">Usage & cost overview</h3>
                                            <p className="mt-1 text-sm text-slate-400">
                                                Estimated EC2 runtime spend based on tracked provision time and common on-demand rates.
                                            </p>
                                        </div>
                                        <p className="text-xs text-slate-500">Rates are estimates, not AWS billing data.</p>
                                    </div>

                                    <div className="mt-4 grid gap-3 md:grid-cols-3">
                                        <div className="rounded-md border border-slate-800 bg-slate-950 p-3">
                                            <p className="text-xs uppercase tracking-wide text-slate-500">Cloud environments</p>
                                            <p className="mt-2 text-2xl font-semibold text-slate-100">{activeCloudEnvironmentCount}</p>
                                            <p className="mt-1 text-xs text-slate-400">Provisioned or deprovisioning environments with tracked cloud state.</p>
                                        </div>
                                        <div className="rounded-md border border-slate-800 bg-slate-950 p-3">
                                            <p className="text-xs uppercase tracking-wide text-slate-500">Estimated accrued spend</p>
                                            <p className="mt-2 text-2xl font-semibold text-slate-100">{formatCurrency(totalEstimatedSpend)}</p>
                                            <p className="mt-1 text-xs text-slate-400">Based on runtime since `cloud_provisioned_at`.</p>
                                        </div>
                                        <div className="rounded-md border border-slate-800 bg-slate-950 p-3">
                                            <p className="text-xs uppercase tracking-wide text-slate-500">Projected monthly run rate</p>
                                            <p className="mt-2 text-2xl font-semibold text-slate-100">{formatCurrency(totalEstimatedMonthly)}</p>
                                            <p className="mt-1 text-xs text-slate-400">Assumes the current instance mix runs 24/7 for 30 days.</p>
                                        </div>
                                    </div>
                                </article>

                                <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                                    <h3 className="font-medium">Cloud environment estimates</h3>
                                    {activeCloudUsage.length === 0 ? (
                                        <p className="mt-2 text-sm text-slate-400">No provisioned cloud environments to estimate yet.</p>
                                    ) : (
                                        <div className="mt-3 space-y-3">
                                            {activeCloudUsage.map(({ environment: env, usage }) => (
                                                <div key={env.id} className="rounded-md border border-slate-800 bg-slate-950 p-3">
                                                    <div className="flex flex-wrap items-center justify-between gap-2">
                                                        <div>
                                                            <p className="font-medium text-slate-100">{env.name}</p>
                                                            <p className="text-xs text-slate-500">
                                                                {env.cloud_instance_type || "Unknown type"}
                                                                {env.cloud_region ? ` | ${env.cloud_region}` : ""}
                                                                {env.instance_id ? ` | ${env.instance_id}` : ""}
                                                            </p>
                                                        </div>
                                                        <span className="rounded-full border border-slate-700 px-2 py-1 text-xs text-slate-300">
                                                            {env.cloud_status}
                                                        </span>
                                                    </div>

                                                    <div className="mt-3 grid gap-3 md:grid-cols-4">
                                                        <div>
                                                            <p className="text-xs uppercase tracking-wide text-slate-500">Runtime</p>
                                                            <p className="mt-1 text-sm text-slate-100">{usage.formattedRuntime}</p>
                                                        </div>
                                                        <div>
                                                            <p className="text-xs uppercase tracking-wide text-slate-500">Hourly rate</p>
                                                            <p className="mt-1 text-sm text-slate-100">
                                                                {usage.hourlyRate !== null ? `${formatCurrency(usage.hourlyRate)}/hr` : "N/A"}
                                                            </p>
                                                        </div>
                                                        <div>
                                                            <p className="text-xs uppercase tracking-wide text-slate-500">Accrued estimate</p>
                                                            <p className="mt-1 text-sm text-slate-100">{formatCurrency(usage.estimatedSpend)}</p>
                                                        </div>
                                                        <div>
                                                            <p className="text-xs uppercase tracking-wide text-slate-500">Monthly run rate</p>
                                                            <p className="mt-1 text-sm text-slate-100">{formatCurrency(usage.estimatedMonthly)}</p>
                                                        </div>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </article>
                            </>
                        ) : null}

                        {activeView === "settings" ? (
                            <article className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                                <h3 className="font-medium">Cloud provisioning defaults</h3>
                                <p className="mt-1 text-sm text-slate-400">
                                    Used when you click Provision on an environment. The key pair name is the name registered in AWS EC2
                                    (for example, <code className="text-slate-300">docklab-key</code>), not your local{" "}
                                    <code className="text-slate-300">.pem</code> file path.
                                </p>

                                <div className="mt-4 grid gap-3 md:grid-cols-2">
                                    <input
                                        className="rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500 focus:ring"
                                        placeholder="AWS region"
                                        value={awsRegion}
                                        onChange={(event) => setAWSRegion(event.target.value)}
                                        maxLength={32}
                                    />
                                    <input
                                        className="rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500 focus:ring"
                                        placeholder="Instance type"
                                        value={instanceType}
                                        onChange={(event) => setInstanceType(event.target.value)}
                                        maxLength={32}
                                    />
                                    <input
                                        className="rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500 focus:ring"
                                        placeholder="AMI ID"
                                        value={amiID}
                                        onChange={(event) => setAMIID(event.target.value)}
                                        maxLength={32}
                                    />
                                    <input
                                        className="rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500 focus:ring"
                                        placeholder="EC2 key pair name (e.g. docklab-key)"
                                        value={keyName}
                                        onChange={(event) => setKeyName(event.target.value)}
                                        maxLength={64}
                                    />
                                </div>
                            </article>
                        ) : null}
                    </section>
                </div>
            </main>

            {confirmDialog.open ? (
                <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/75 px-4">
                    <div className="w-full max-w-md rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl">
                        <h3 className="text-lg font-semibold text-slate-100">{confirmDialog.title}</h3>
                        <p className="mt-2 text-sm text-slate-300">{confirmDialog.description}</p>
                        <div className="mt-5 flex justify-end gap-2">
                            <button
                                className="rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-200 hover:bg-slate-800"
                                type="button"
                                onClick={closeConfirmDialog}
                            >
                                Cancel
                            </button>
                            <button
                                className="rounded-md bg-rose-600 px-3 py-1.5 text-sm font-medium text-rose-50 hover:bg-rose-500"
                                type="button"
                                onClick={() => void handleConfirmAction()}
                            >
                                {confirmDialog.confirmLabel}
                            </button>
                        </div>
                    </div>
                </div>
            ) : null}
        </>
    );
}
