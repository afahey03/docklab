import { Navigate, Route, Routes } from "react-router-dom";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { DashboardPage } from "./pages/DashboardPage";
import { LoginPage } from "./pages/LoginPage";
import { RegisterPage } from "./pages/RegisterPage";

function App() {
    return (
        <Routes>
            <Route path="/" element={<Navigate replace to="/dashboard" />} />

            <Route element={<ProtectedRoute />}>
                <Route element={<DashboardPage />} path="/dashboard" />
            </Route>

            <Route element={<LoginPage />} path="/login" />
            <Route element={<RegisterPage />} path="/register" />
        </Routes>
    );
}

export default App;
