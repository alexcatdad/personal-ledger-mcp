import { createFileRoute } from "@tanstack/react-router";
import {
  QueryClient,
  QueryCache,
  QueryClientProvider,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useState, type FormEvent } from "react";
import createClient from "openapi-fetch";
import type { components, paths } from "@/lib/api.gen";
import {
  ArrowDownLeft,
  ArrowUpRight,
  ArrowLeftRight,
  ChevronDown,
  LockKeyhole,
  LogOut,
  RefreshCw,
  Wallet,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
export const Route = createFileRoute("/")({ component: App });
const api = createClient<paths>({ baseUrl: "" });
class AccessDenied extends Error {}
api.use({
  onResponse({ response }) {
    if (response.status === 401 || response.status === 403) {
      throw new AccessDenied("Your access could not be verified.");
    }
  },
});
function authHeaders(token: string) {
  return token ? { Authorization: `Bearer ${token}` } : {};
}
type AccessSession = { mode: "bearer" } | { mode: "tailscale"; login: string };
// Money arrives as exact decimal strings. Formatting never converts it through floating point.
function money(value: string) {
  const [whole, fraction = "00"] = value.split(".");
  return `${whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",")}.${fraction}`;
}
function App() {
  const [token, setToken] = useState("");
  const [session, setSession] = useState<AccessSession | null>(null);
  const [accessDenied, setAccessDenied] = useState(false);
  const [client] = useState(
    () =>
      new QueryClient({
        queryCache: new QueryCache({
          onError(error) { if (error instanceof AccessDenied) setAccessDenied(true); },
        }),
        defaultOptions: {
          queries: { retry: false, refetchOnWindowFocus: true, staleTime: 15_000, gcTime: 0 },
        },
      }),
  );
  useEffect(() => {
    const controller = new AbortController();
    fetch("/auth/session", { cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error("Access denied");
        const value = await response.json();
        if (value.mode !== "bearer" && !(value.mode === "tailscale" && typeof value.login === "string" && value.login)) throw new Error("Invalid authentication configuration");
        setSession(value);
      })
      .catch(() => { if (!controller.signal.aborted) setAccessDenied(true); });
    return () => controller.abort();
  }, []);
  useEffect(() => {
    if (accessDenied) { client.clear(); setToken(""); }
  }, [accessDenied, client]);
  function lock() {
    setToken("");
    client.clear();
  }
  return (
    <QueryClientProvider client={client}>
      <div className="app-shell">
        <header className="topbar">
          <a href="/" className="wordmark">
            <span className="brand-mark" aria-hidden="true">
              l.
            </span>
            ledger<span className="personal">PERSONAL</span>
          </a>
          {session?.mode === "tailscale" && !accessDenied ? (
            <span className="private-label"><LockKeyhole size={13} /> {session.login}</span>
          ) : token ? (
            <Button variant="ghost" onClick={lock}>
              <LogOut />
              Lock
            </Button>
          ) : (
            <span className="private-label">
              <LockKeyhole size={13} /> Private ledger
            </span>
          )}
        </header>
        {accessDenied ? (
          <main className="unlock-panel">
            <h1>Access could not be verified.</h1>
            <p>Reconnect to Tailscale with your allowed account, or check the access configuration for this ledger.</p>
            <Button onClick={() => window.location.reload()}>Try again</Button>
          </main>
        ) : !session ? (
          <main><p className="loading">Verifying your access…</p></main>
        ) : session.mode === "tailscale" || token ? (
          <DashboardView token={token} />
        ) : (
          <Unlock onUnlock={setToken} />
        )}
        <footer>
          YOUR FINANCES, IN FOCUS
          <span>Amounts stay in their original currency.</span>
        </footer>
      </div>
    </QueryClientProvider>
  );
}
function Unlock({ onUnlock }: { onUnlock: (token: string) => void }) {
  const [value, setValue] = useState("");
  function submit(event: FormEvent) {
    event.preventDefault();
    if (value.trim()) {
      onUnlock(value.trim());
      setValue("");
    }
  }
  return (
    <main className="unlock">
      <div className="unlock-icon">
        <LockKeyhole size={24} />
      </div>
      <p className="eyebrow">A QUIET PLACE FOR YOUR NUMBERS</p>
      <h1>
        Your money.
        <br />
        <span>A clearer view.</span>
      </h1>
      <p className="intro">
        Balances, spending and the details behind them.
        <br />
        Unlock your ledger to see where things stand.
      </p>
      <form onSubmit={submit} className="unlock-form">
        <label htmlFor="access-token">Access token</label>
        <Input
          id="access-token"
          type="password"
          autoComplete="off"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="Enter your private token"
          required
        />
        <Button type="submit" disabled={!value.trim()}>
          Open ledger <ArrowUpRight />
        </Button>
        <p className="hint">
          Kept in this tab only. Lock the ledger when you’re done.
        </p>
      </form>
    </main>
  );
}
const defaultHistoryFilters = { account_id: "", from_date: "", to_date: "", status: "posted", currency: "" };
const defaultExpenseFilters = { account_id: "", category_id: "", project_id: "", currency: "", from_date: "", to_date: "", status: "posted", payment_state: "" };
function DashboardView({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const [incomeFilters, setIncomeFilters] = useState(defaultHistoryFilters);
  const [incomeOffset, setIncomeOffset] = useState(0);
  const [transferFilters, setTransferFilters] = useState(defaultHistoryFilters);
  const [transferOffset, setTransferOffset] = useState(0);
  const [debtStatus, setDebtStatus] = useState("open");
  const [debtOffset, setDebtOffset] = useState(0);
  const [expenseFilters, setExpenseFilters] = useState(defaultExpenseFilters);
  const [expenseOffset, setExpenseOffset] = useState(0);
  function filterExpenses(key: keyof typeof defaultExpenseFilters, value: string) {
    setExpenseFilters((current) => ({ ...current, [key]: value }));
    setExpenseOffset(0);
  }
  const expenses = useQuery({
    queryKey: ["expenses", expenseFilters, expenseOffset],
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/expenses", {
        params: { query: { ...expenseFilters, limit: 20, offset: expenseOffset } },
        headers: authHeaders(token), signal, cache: "no-store",
      });
      if (!response.ok || !data) throw new Error((response.status === 400 || response.status === 422) ? "Check the expense filters, including the date range." : "Expenses could not be loaded. Check your connection and access, then try again.");
      return { ...data, expenses: (data.expenses ?? []).map((expense) => ({ ...expense, allocations: expense.allocations ?? [] })), totals: data.totals ?? [], matching_allocation_totals: data.matching_allocation_totals ?? [] };
    },
  });
  const query = useQuery({
    queryKey: ["dashboard"],
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/dashboard", {
        headers: authHeaders(token),
        signal,
        cache: "no-store",
      });
      if (!response.ok || !data)
        throw new Error(
          response.status === 401
            ? "Your access could not be verified. Reload the ledger to reconnect."
            : "Your ledger could not be loaded. Check that the service is running, then try again.",
        );
      return {
        ...data,
        accounts: data.accounts ?? [],
        categories: data.categories ?? [],
        projects: data.projects ?? [],
        spending: data.spending ?? [],
        expenses: (data.expenses ?? []).map((expense) => ({
          ...expense,
          allocations: expense.allocations ?? [],
        })),
      };
    },
  });
  const intents = useQuery({
    queryKey: ["buying-intents"],
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/buying-intents", {
        params: { query: { status: "all", limit: 20 } },
        headers: authHeaders(token),
        signal,
        cache: "no-store",
      });
      if (!response.ok || !data)
        throw new Error(
          response.status === 401
            ? "Your access could not be verified. Reload the ledger to reconnect."
            : "Buying intents could not be loaded. Check that the service is running, then refresh.",
        );
      return { ...data, intents: data.intents ?? [] };
    },
  });
  const income = useQuery({
    queryKey: ["income", incomeFilters, incomeOffset],
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/income", {
        params: { query: { ...incomeFilters, limit: 20, offset: incomeOffset } },
        headers: authHeaders(token),
        signal,
        cache: "no-store",
      });
      if (!response.ok || !data)
        throw new Error(
          response.status === 401
            ? "Your access could not be verified. Reload the ledger to reconnect."
            : response.status === 400 || response.status === 422 ? "Check your income filters, including the date range." : "Income could not be loaded. Check that the service is running, then refresh.",
        );
      return { ...data, incomes: data.incomes ?? [], totals: data.totals ?? [] };
    },
  });
  const transfers = useQuery({
    queryKey: ["transfers", transferFilters, transferOffset],
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/transfers", {
        params: { query: { account_id: transferFilters.account_id, from_date: transferFilters.from_date, to_date: transferFilters.to_date, status: transferFilters.status, limit: 20, offset: transferOffset } },
        headers: authHeaders(token),
        signal,
        cache: "no-store",
      });
      if (!response.ok || !data)
        throw new Error(
          response.status === 401
            ? "Your access could not be verified. Reload the ledger to reconnect."
            : response.status === 400 || response.status === 422 ? "Check your transfer filters, including the date range." : "Transfers could not be loaded. Check that the service is running, then refresh.",
        );
      return { ...data, transfers: data.transfers ?? [] };
    },
  });
  const receivables = useQuery({
    queryKey: ["receivables", debtStatus, debtOffset],
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/receivables", {
        params: { query: { status: debtStatus, limit: 20, offset: debtOffset } },
        headers: authHeaders(token),
        signal,
        cache: "no-store",
      });
      if (!response.ok || !data)
        throw new Error(
          response.status === 401
            ? "Your access could not be verified. Reload the ledger to reconnect."
            : "Money owed to you could not be loaded. Check that the service is running, then refresh.",
        );
      return { ...data, receivables: data.receivables ?? [], outstanding_totals: data.outstanding_totals ?? [] };
    },
  });
  const observations = useQuery({
    queryKey: ["balance-observations"],
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/balance-observations", {
        params: { query: { limit: 20 } },
        headers: authHeaders(token),
        signal,
        cache: "no-store",
      });
      if (!response.ok || !data)
        throw new Error(
          response.status === 401
            ? "Your access could not be verified. Reload the ledger to reconnect."
            : "Balance observations could not be loaded. Check that the service is running, then refresh.",
        );
      return { ...data, observations: data.observations ?? [] };
    },
  });
  const refreshing = expenses.isFetching || query.isFetching || intents.isFetching || income.isFetching || transfers.isFetching || receivables.isFetching || observations.isFetching;
  function refresh() {
    void expenses.refetch();
    void query.refetch();
    void intents.refetch();
    void income.refetch();
    void transfers.refetch();
    void receivables.refetch();
    void queryClient.invalidateQueries({ queryKey: ["repayments"] });
    void queryClient.invalidateQueries({ queryKey: ["monthly-report"] });
    void observations.refetch();
  }
  const data = query.data;
  const categoryNames = new Map(
    data?.categories.map((item) => [item.id, item.name]),
  );
  const projectNames = new Map(
    data?.projects.map((item) => [item.id, item.name]),
  );
  const accountNames = new Map(
    data?.accounts.map((item) => [item.id, item.name]),
  );
  return (
    <main className="dashboard">
      <section className="page-heading">
        <div>
          <p className="eyebrow">YOUR LEDGER</p>
          <h1>
            At a glance<span className="heading-dot">.</span>
          </h1>
          <p className="intro">
            Your balances and spending, with the details in view.
          </p>
        </div>
        <Button
          variant="outline"
          onClick={refresh}
          disabled={refreshing}
        >
          <RefreshCw className={refreshing ? "refreshing" : ""} />
          Refresh
        </Button>
      </section>
      {query.isPending ? (
        <div className="state-box" role="status">
          Loading your balances and spending…
        </div>
      ) : query.isError ? (
        <div className="state-box error" role="alert">
          <h2>Unable to open your ledger</h2>
          <p>{query.error.message}</p>
          <Button variant="outline" onClick={() => query.refetch()}>
            Try again
          </Button>
        </div>
      ) : data ? (
        <>
          <section aria-labelledby="accounts-heading">
            <div className="section-heading">
              <h2 id="accounts-heading">Accounts</h2>
              <span>Current balances</span>
            </div>
            <div className="accounts">
              {data.accounts.length ? (
                data.accounts.map((account) => (
                  <article className="account" key={account.id}>
                    <div className="account-top">
                      <Wallet size={18} />
                      <span>{account.currency}</span>
                    </div>
                    <h3>{account.name}</h3>
                    {account.archived ? <p className="hint">Archived · History retained</p> : null}
                    <p className="account-balance">
                      {money(account.balance)}
                      <span>{account.currency}</span>
                    </p>
                    {account.balance_quality && account.balance_quality !== "exact" ? (
                      <p className="hint">
                        {account.balance_quality === "incomplete" ? "Incomplete balance" : "Estimated balance"}
                        {account.estimated_expense_count ? ` · ${account.estimated_expense_count} estimated expense${account.estimated_expense_count === 1 ? "" : "s"}` : ""}
                        {account.unresolved_expense_count ? ` · ${account.unresolved_expense_count} unresolved expense${account.unresolved_expense_count === 1 ? "" : "s"} excluded` : ""}
                      </p>
                    ) : null}
                    <p className="account-since">Tracked since {account.opening_date}</p>
                  </article>
                ))
              ) : (
                <div className="empty">
                  No accounts yet. Ask your assistant to create an account with
                  its opening balance.
                </div>
              )}
            </div>
          </section>
          <div className="lower-grid">
            <section className="activity" aria-labelledby="activity-heading">
              <div className="section-heading">
                <h2 id="activity-heading">Expense activity</h2>
                <span>
                  {expenses.data ? `${expenses.data.total_count} matching expenses` : "Filtered activity"}
                </span>
              </div>
              <details className="expense-filters">
                <summary>Filter expenses</summary>
                <div className="expense-filter-grid">
                  <label>Account<select value={expenseFilters.account_id} onChange={(e) => filterExpenses("account_id", e.target.value)}><option value="">All accounts</option>{data.accounts.map((item) => <option key={item.id} value={item.id}>{item.name}{item.archived ? " (archived)" : ""}</option>)}</select></label>
                  <label>Category<select value={expenseFilters.category_id} onChange={(e) => filterExpenses("category_id", e.target.value)}><option value="">All categories</option>{data.categories.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
                  <label>Project<select value={expenseFilters.project_id} onChange={(e) => filterExpenses("project_id", e.target.value)}><option value="">All projects</option>{data.projects.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
                  <label>Original currency<select value={expenseFilters.currency} onChange={(e) => filterExpenses("currency", e.target.value)}><option value="">All currencies</option>{[...new Set(["RON", "EUR", "USD", ...data.accounts.map((item) => item.currency)])].map((currency) => <option key={currency} value={currency}>{currency}</option>)}</select></label>
                  <label>From date<Input type="date" value={expenseFilters.from_date} onChange={(e) => filterExpenses("from_date", e.target.value)} /></label>
                  <label>Through date<Input type="date" value={expenseFilters.to_date} onChange={(e) => filterExpenses("to_date", e.target.value)} /></label>
                  <label>Status<select value={expenseFilters.status} onChange={(e) => filterExpenses("status", e.target.value)}><option value="posted">Posted</option><option value="all">All</option><option value="void">Voided</option></select></label>
                  <label>Account payment<select value={expenseFilters.payment_state} onChange={(e) => filterExpenses("payment_state", e.target.value)}><option value="">All payment states</option><option value="exact">Exact</option><option value="estimated">Estimated</option><option value="unresolved">Unresolved</option><option value="needs_attention">Needs attention</option></select></label>
                </div>
                <Button variant="ghost" onClick={() => { setExpenseFilters(defaultExpenseFilters); setExpenseOffset(0); }}>Reset filters</Button>
              </details>
              {expenses.data && !expenses.isError ? (
                <div className="expense-scope-totals">
                  <p className="hint">Full matching expense totals · Posted only · All pages</p>
                  <p>{expenses.data.totals.length ? expenses.data.totals.map((item) => `${money(item.amount)} ${item.currency}`).join(" · ") : "No posted spending in this selection"}</p>
                  {expenseFilters.category_id || expenseFilters.project_id ? (
                    <details><summary>Matching allocation totals</summary>
                      <p className="hint">Only the category/project lines matching your filters, across all pages. Complete expense totals above can be larger.</p>
                      {expenses.data.matching_allocation_totals.map((item) => <div className="allocation" key={`${item.category_id}:${item.project_id}:${item.currency}`}><span>{categoryNames.get(item.category_id) ?? "Category"}{item.project_id ? ` · ${projectNames.get(item.project_id) ?? "Project"}` : ""}</span><span>{money(item.amount)} {item.currency}</span></div>)}
                    </details>
                  ) : null}
                </div>
              ) : null}
              {expenses.isPending ? <div className="state-box" role="status">Loading expenses…</div> : expenses.isError ? <div className="state-box error" role="alert"><p>{expenses.error.message}</p><Button variant="outline" onClick={() => expenses.refetch()}>Try again</Button></div> : expenses.data?.expenses.length ? (
                <div className="expense-list">
                  {expenses.data.expenses.map((expense) => (
                    <details
                      key={expense.id}
                      className={`expense ${expense.status === "void" ? "voided" : ""}`}
                    >
                      <summary>
                        <span className="expense-icon">
                          <ArrowDownLeft size={18} />
                        </span>
                        <span className="expense-description">
                          <strong>{expense.description}</strong>
                          <span>
                            {expense.date.slice(0, 10)} ·{" "}
                            {accountNames.get(expense.account_id) ?? "Account"}
                            {expense.status === "void" ? " · Voided" : ""}
                          </span>
                          {expense.payment && expense.payment.currency !== expense.currency ? (
                            <span>
                              Account debit: {expense.payment.state === "unresolved" || !expense.payment.amount ? `unresolved (${expense.payment.currency})` : `${money(expense.payment.amount)} ${expense.payment.currency}`}
                              {expense.payment.state === "estimated" ? " · Estimated" : expense.payment.state === "exact" ? " · Exact bank charge" : ""}
                            </span>
                          ) : null}
                        </span>
                        <span className="expense-amount">
                          {money(expense.amount)}
                          <small>{expense.currency}</small>
                        </span>
                        <ChevronDown className="expand-icon" size={15} />
                      </summary>
                      <div className="allocations">
                        <p className="eyebrow">
                          EXPENSE BREAKDOWN · REVISION {expense.version}
                        </p>
                        <ExpensePayment expense={expense} />
                        {expense.allocations.map((allocation, index) => (
                          <div className="allocation" key={index}>
                            <span>
                              {categoryNames.get(allocation.category_id) ??
                                "Category"}
                              {allocation.project_id ? (
                                <small>
                                  {projectNames.get(allocation.project_id) ??
                                    "Project"}
                                </small>
                              ) : null}
                            </span>
                            <span>
                              {money(allocation.amount)} {expense.currency}
                            </span>
                          </div>
                        ))}
                      </div>
                    </details>
                  ))}
                </div>
              ) : (
                <div className="empty">
                  <h3>No expenses match this selection</h3>
                  <p>Try changing the filters, or tell your assistant about an expense to record.</p>
                </div>
              )}
              <nav className="expense-pagination" aria-label="Expense pages">
                <Button variant="outline" disabled={expenseOffset === 0 || expenses.isFetching} onClick={() => setExpenseOffset((offset) => Math.max(0, offset - 20))}>Previous</Button>
                <span>Page {Math.floor(expenseOffset / 20) + 1}{expenses.data ? ` of ${Math.max(1, Math.ceil(expenses.data.total_count / 20))}` : ""}</span>
                <Button variant="outline" disabled={!expenses.data || expenseOffset + 20 >= expenses.data.total_count || expenses.isFetching} onClick={() => setExpenseOffset((offset) => offset + 20)}>Next</Button>
              </nav>
            </section>
            <aside aria-labelledby="projects-heading">
              <div className="section-heading">
                <h2 id="projects-heading">Project spending</h2>
                <span>All time</span>
              </div>
              <div className="projects">
                {data.spending.length ? (
                  data.spending.map((item) => (
                    <div
                      key={`${item.project_id}:${item.currency}`}
                      className="project"
                    >
                      <span className="project-mark" aria-hidden="true">
                        {(projectNames.get(item.project_id) ?? "P").slice(0, 1)}
                      </span>
                      <div>
                        <h3>
                          {projectNames.get(item.project_id) ?? "Unassigned"}
                        </h3>
                        <p>
                          {money(item.amount)} <span>{item.currency}</span>
                        </p>
                      </div>
                    </div>
                  ))
                ) : (
                  <p className="empty">
                    Spending linked to a project will appear here.
                  </p>
                )}
              </div>
              <div className="assistant-note">
                <span className="note-rule" />
                <p>Keep the conversation going.</p>
                <span>
                  Your assistant records and corrects your finances. This is
                  your place to review them.
                </span>
              </div>
            </aside>
          </div>
        </>
      ) : null}
      <ProjectReportPanel token={token} projects={data?.projects ?? []} categories={data?.categories ?? []} />
      <MonthlyReportPanel token={token} accounts={data?.accounts ?? []} />
      <section className="buying-intents" aria-labelledby="income-heading">
        <div className="section-heading">
          <h2 id="income-heading">Income</h2>
          {income.data && !income.isError ? (
            <span>Showing {income.data.incomes.length} of {income.data.total_count} · {incomeFilters.status}</span>
          ) : null}
        </div>
        <p className="intent-intro">Money received, excluding opening balances and transfers.</p>
        <HistoryFilters label="income" filters={incomeFilters} accounts={data?.accounts ?? []} includeCurrency onChange={(filters) => { setIncomeFilters(filters); setIncomeOffset(0); }} />
        {income.data && !income.isError ? (
            <p className="intent-intro">
              Matching posted income · All pages: {income.data.totals.length ? income.data.totals.map((total) => `${money(total.amount)} ${total.currency}`).join(" · ") : "No posted income"}
            </p>
        ) : null}
        {income.isPending ? (
          <div className="state-box" role="status">Loading income…</div>
        ) : income.isError ? (
          <div className="state-box error" role="alert">
            <h3>Unable to load income</h3>
            <p>{income.error.message}</p>
            <Button variant="outline" onClick={() => income.refetch()}>Try again</Button>
          </div>
        ) : income.data?.incomes.length ? (
          <>
            <div className="expense-list">
              {income.data.incomes.map((item) => (
                <details key={item.id} className={`expense ${item.status === "void" ? "voided" : ""}`}>
                  <summary>
                    <span className="expense-icon"><ArrowUpRight size={18} /></span>
                    <span className="expense-description">
                      <strong>{item.description}</strong>
                      <span>{item.date.slice(0, 10)} · {accountNames.get(item.account_id) ?? "Account"} · {item.status === "void" ? "Voided" : "Posted"}</span>
                    </span>
                    <span className="expense-amount">+{money(item.amount)}<small>{item.currency}</small></span>
                    <ChevronDown className="expand-icon" size={15} />
                  </summary>
                  <div className="allocations">
                    <p className="eyebrow">INCOME · REVISION {item.version}</p>
                    <p className="hint">{accountNames.get(item.account_id) ?? `Account ${item.account_id}`} · {item.date.slice(0, 10)}</p>
                  </div>
                </details>
              ))}
            </div>
          </>
        ) : (
          <div className="empty">No income matches this selection. Change the filters or tell your assistant about money you’ve received.</div>
        )}
        <HistoryPages label="Income" offset={incomeOffset} total={income.data?.total_count} busy={income.isFetching} onChange={setIncomeOffset} />
      </section>
      <section className="buying-intents" aria-labelledby="transfers-heading">
        <div className="section-heading">
          <h2 id="transfers-heading">Transfers</h2>
          {transfers.data && !transfers.isError ? (
            <span>Showing {transfers.data.transfers.length} of {transfers.data.total_count} · {transferFilters.status}</span>
          ) : null}
        </div>
        <p className="intent-intro">Moves between your accounts, excluded from income and spending. Fees are recorded as separate expenses.</p>
        <HistoryFilters label="transfers" filters={transferFilters} accounts={data?.accounts ?? []} onChange={(filters) => { setTransferFilters(filters); setTransferOffset(0); }} />
        {transfers.isPending ? (
          <div className="state-box" role="status">Loading transfers…</div>
        ) : transfers.isError ? (
          <div className="state-box error" role="alert">
            <h3>Unable to load transfers</h3>
            <p>{transfers.error.message}</p>
            <Button variant="outline" onClick={() => transfers.refetch()}>Try again</Button>
          </div>
        ) : transfers.data?.transfers.length ? (
          <div className="expense-list">
            {transfers.data.transfers.map((item) => (
              <details key={item.id} className={`expense ${item.status === "void" ? "voided" : ""}`}>
                <summary>
                  <span className="expense-icon"><ArrowLeftRight size={18} /></span>
                  <span className="expense-description">
                    <strong>{item.description}</strong>
                    <span>{item.date.slice(0, 10)} · {item.status === "void" ? "Voided" : "Posted"}</span>
                    <span>{accountNames.get(item.source_account_id) ?? "Source account"} → {accountNames.get(item.destination_account_id) ?? "Destination account"}</span>
                  </span>
                  <span className="expense-amount">
                    −{money(item.source_amount)}<small>{item.source_currency} sent</small>
                    +{money(item.destination_amount)}<small>{item.destination_currency} received</small>
                  </span>
                  <ChevronDown className="expand-icon" size={15} />
                </summary>
                <div className="allocations">
                  <p className="eyebrow">TRANSFER · REVISION {item.version}</p>
                  <div className="allocation">
                    <span>From {accountNames.get(item.source_account_id) ?? `account ${item.source_account_id}`}</span>
                    <span>{money(item.source_amount)} {item.source_currency}</span>
                  </div>
                  <div className="allocation">
                    <span>To {accountNames.get(item.destination_account_id) ?? `account ${item.destination_account_id}`}</span>
                    <span>{money(item.destination_amount)} {item.destination_currency}</span>
                  </div>
                </div>
              </details>
            ))}
          </div>
        ) : (
          <div className="empty">No transfers match this selection. Change the filters or tell your assistant about money moved between accounts.</div>
        )}
        <HistoryPages label="Transfer" offset={transferOffset} total={transfers.data?.total_count} busy={transfers.isFetching} onChange={setTransferOffset} />
      </section>
      <section className="buying-intents" aria-labelledby="receivables-heading">
        <div className="section-heading">
          <h2 id="receivables-heading">Money owed to you</h2>
          {receivables.data && !receivables.isError ? (
            <span>Showing {receivables.data.receivables.length} of {receivables.data.total_count} · {debtStatus}</span>
          ) : null}
        </div>
        <p className="intent-intro">Outstanding debts and the repayments already received.</p>
        <div className="expense-filter-grid">
          <label>Debt status<select value={debtStatus} onChange={(event) => { setDebtStatus(event.target.value); setDebtOffset(0); }}><option value="open">Open</option><option value="settled">Settled</option><option value="void">Voided</option><option value="all">All</option></select></label>
        </div>
        {receivables.isPending ? (
          <div className="state-box" role="status">Loading money owed to you…</div>
        ) : receivables.isError ? (
          <div className="state-box error" role="alert">
            <h3>Unable to load money owed to you</h3>
            <p>{receivables.error.message}</p>
            <Button variant="outline" onClick={() => receivables.refetch()}>Try again</Button>
          </div>
        ) : receivables.data?.receivables.length ? (
          <>
            <p className="intent-intro">
              Total outstanding for this selection · All pages: {receivables.data.outstanding_totals.map((total) => `${money(total.amount)} ${total.currency}`).join(" · ")}
            </p>
            <div className="expense-list">
              {receivables.data.receivables.map((item) => (
                <ReceivableRow key={item.id} item={item} token={token} accountNames={accountNames} />
              ))}
            </div>
          </>
        ) : (
          <div className="empty">No debts match this status. Choose another status, or tell your assistant when someone owes you money.</div>
        )}
        <nav className="expense-pagination" aria-label="Receivable pages">
          <Button variant="outline" disabled={debtOffset === 0 || receivables.isFetching} onClick={() => setDebtOffset((offset) => Math.max(0, offset - 20))}>Previous</Button>
          <span>Page {Math.floor(debtOffset / 20) + 1}{receivables.data ? ` of ${Math.max(1, Math.ceil(receivables.data.total_count / 20))}` : ""}</span>
          <Button variant="outline" disabled={!receivables.data || debtOffset + 20 >= receivables.data.total_count || receivables.isFetching} onClick={() => setDebtOffset((offset) => offset + 20)}>Next</Button>
        </nav>
      </section>
      <section className="buying-intents" aria-labelledby="reconciliation-heading">
        <div className="section-heading">
          <h2 id="reconciliation-heading">Reconciliation</h2>
          {observations.data && !observations.isError ? (
            <span>Showing {observations.data.observations.length} of {observations.data.total_count}</span>
          ) : null}
        </div>
        <p className="intent-intro">Posted balance at end of day. Each observation compares the actual balance with the ledger for that date.</p>
        {observations.isPending ? (
          <div className="state-box" role="status">Loading balance observations…</div>
        ) : observations.isError ? (
          <div className="state-box error" role="alert">
            <h3>Unable to load balance observations</h3>
            <p>{observations.error.message}</p>
            <Button variant="outline" onClick={() => observations.refetch()}>Try again</Button>
          </div>
        ) : observations.data?.observations.length ? (
          <div className="expense-list">
            {observations.data.observations.map((item) => (
              <details key={item.id} className={`expense ${item.status === "void" ? "voided" : ""}`}>
                <summary>
                  <span className="expense-icon"><Wallet size={18} /></span>
                  <span className="expense-description">
                    <strong>{accountNames.get(item.account_id) ?? "Account"}</strong>
                    <span>{item.as_of_date.slice(0, 10)} · {item.status === "void" ? "Voided observation" : item.balance_quality === "incomplete" ? "Incomplete comparison" : item.balance_quality === "estimated" ? "Estimated comparison" : item.status === "matched" ? "Matched" : "Unmatched"}</span>
                    <span>Actual {money(item.actual_balance)} {item.currency} · Ledger {money(item.current_ledger_balance)} {item.currency}</span>
                  </span>
                  <span className="expense-amount">
                    {item.difference.startsWith("-") ? "" : "+"}{money(item.difference)}
                    <small>{item.currency} difference{item.status === "void" ? " · void" : ""}</small>
                  </span>
                  <ChevronDown className="expand-icon" size={15} />
                </summary>
                <div className="allocations">
                  <p className="eyebrow">BALANCE OBSERVATION · REVISION {item.version}</p>
                  {item.balance_quality && item.balance_quality !== "exact" ? (
                    <p className="intent-note">
                      {item.balance_quality === "incomplete" ? "The ledger balance is incomplete." : "The ledger balance includes estimated payments."}
                      {item.estimated_expense_count ? ` ${item.estimated_expense_count} estimated expense${item.estimated_expense_count === 1 ? "" : "s"}.` : ""}
                      {item.unresolved_expense_count ? ` ${item.unresolved_expense_count} unresolved expense${item.unresolved_expense_count === 1 ? " is" : "s are"} excluded.` : ""}
                      {" "}A zero difference does not confirm reconciliation until these payments are resolved.
                    </p>
                  ) : null}
                  <p className="hint">Posted balance at end of day · {item.as_of_date.slice(0, 10)}</p>
                  <div className="allocation">
                    <span>Ledger snapshot from recording or correction</span>
                    <span>{money(item.recorded_ledger_balance)} {item.currency}</span>
                  </div>
                  <div className="allocation">
                    <span>Ledger now for the same date</span>
                    <span>{money(item.current_ledger_balance)} {item.currency}</span>
                  </div>
                  <p className="intent-note">{item.notes || "No notes recorded."}</p>
                  <p className="hint">{item.status === "void" ? "This observation was voided and is retained for reference." : "Difference is actual balance minus the current ledger balance for this date. Recording an observation does not change your balance."}</p>
                </div>
              </details>
            ))}
          </div>
        ) : (
          <div className="empty">No balance observations yet. Tell your assistant an account’s posted balance and date to compare it with your ledger.</div>
        )}
      </section>
      <section className="buying-intents" aria-labelledby="intents-heading">
        <div className="section-heading">
          <h2 id="intents-heading">Buying intents</h2>
          {intents.data && !intents.isError ? (
            <span>Showing {intents.data.intents.length} of {intents.data.total_count} · First 20</span>
          ) : null}
        </div>
        <p className="intent-intro">Offers you’re considering and the decisions you’ve made.</p>
        {intents.isPending ? (
          <div className="state-box" role="status">Loading buying intents…</div>
        ) : intents.isError ? (
          <div className="state-box error" role="alert">
            <h3>Unable to load buying intents</h3>
            <p>{intents.error.message}</p>
            <Button variant="outline" onClick={() => intents.refetch()}>Try again</Button>
          </div>
        ) : intents.data?.intents.length ? (
          <div className="intent-list">
            {intents.data.intents.map((intent) => <BuyingIntentRow key={intent.id} intent={intent} />)}
          </div>
        ) : (
          <div className="empty">No buying intents yet. Tell your assistant about something you’re considering buying.</div>
        )}
      </section>
    </main>
  );
}

function BuyingIntentRow({ intent }: { intent: components["schemas"]["BuyingIntent"] }) {
  return (
    <details className="intent">
      <summary>
        <strong>{intent.title}</strong>
        <span className="intent-status">{intent.status}</span>
        <ChevronDown className="expand-icon" size={15} />
      </summary>
      <div className="intent-details">
        {intent.notes ? <p className="intent-note">{intent.notes}</p> : null}
        <div className="intent-columns">
          <div>
            <p className="eyebrow">ADVERTISED OFFERS</p>
            {(intent.candidates ?? []).length ? intent.candidates?.map((candidate) => (
              <div className="intent-offer" key={candidate.id}>
                <p className="intent-reference">{candidate.reference}</p>
                <p className="intent-price">
                  {candidate.advertised_amount ? `${money(candidate.advertised_amount)} ${candidate.currency}` : "Price not recorded"}
                  {intent.candidate_id === candidate.id ? <span>Selected offer</span> : null}
                </p>
                {candidate.notes ? <p className="intent-note">{candidate.notes}</p> : null}
              </div>
            )) : <p className="intent-note">No offers recorded.</p>}
          </div>
          <div>
            <p className="eyebrow">RECORDED DECISION</p>
            {intent.outcome_note ? <p className="intent-note">{intent.outcome_note}</p> : null}
            {intent.expense ? (
              <div className="intent-purchase">
                <p className="intent-price">{money(intent.expense.amount)} {intent.expense.currency}</p>
                <p className="intent-note">{intent.expense.description}</p>
                <ExpensePayment expense={intent.expense} />
                <p className="intent-note">{intent.expense.date} · {intent.expense.status === "void" ? "Expense voided · excluded from spending" : "Recorded expense"}</p>
                <p className="hint">Current expense · revision {intent.expense.version}</p>
              </div>
            ) : <p className="intent-note">{intent.status === "cancelled" ? "Cancelled without a purchase." : "No purchase recorded."}</p>}
          </div>
        </div>
      </div>
    </details>
  );
}

function ExpensePayment({ expense }: { expense: components["schemas"]["Expense"] }) {
  const payment = expense.payment;
  if (!payment || payment.currency === expense.currency) return null;
  return (
    <div className="intent-offer">
      <p className="intent-note">Original expense: {money(expense.amount)} {expense.currency}</p>
      <p className="intent-price">
        Account debit: {payment.state === "unresolved" || !payment.amount ? `unresolved (${payment.currency})` : `${money(payment.amount)} ${payment.currency}`}
        <span>{payment.state === "exact" ? "Exact bank charge" : payment.state === "estimated" ? "Estimated conversion" : "Not included in the account balance yet"}</span>
      </p>
      {payment.evidence ? (
        <p className="hint">
          {payment.evidence.source} · Rate {payment.evidence.rate}
          {payment.evidence.effective_at ? ` · Effective ${payment.evidence.effective_at}` : ""}
          {payment.evidence.retrieved_at ? ` · Retrieved ${payment.evidence.retrieved_at}` : ""}
          {payment.evidence.reference ? ` · ${payment.evidence.reference}` : ""}
        </p>
      ) : null}
    </div>
  );
}

function ReceivableRow({ item, token, accountNames }: { item: components["schemas"]["Receivable"]; token: string; accountNames: Map<string, string> }) {
  const [expanded, setExpanded] = useState(false);
  const [offset, setOffset] = useState(0);
  const repayments = useQuery({
    queryKey: ["repayments", item.id, offset],
    enabled: expanded,
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/repayments", {
        params: { query: { receivable_id: item.id, status: "all", limit: 20, offset } },
        headers: authHeaders(token), signal, cache: "no-store",
      });
      if (!response.ok || !data) throw new Error("Repayments could not be loaded. Check your connection and access, then try again.");
      return { ...data, repayments: data.repayments ?? [] };
    },
  });
  return (
    <details className={`expense ${item.status === "void" ? "voided" : ""}`} onToggle={(event) => setExpanded(event.currentTarget.open)}>
      <summary>
        <span className="expense-icon"><ArrowDownLeft size={18} /></span>
        <span className="expense-description">
          <strong>{item.debtor}</strong>
          <span>{money(item.repaid_amount)} {item.currency} repaid · {money(item.amount)} {item.currency} originally owed</span>
          <span>{item.due_date ? `Due ${item.due_date.slice(0, 10)}` : "No due date"} · {item.status === "void" ? "Voided" : item.status === "settled" ? "Settled" : "Open"}</span>
        </span>
        <span className="expense-amount">{money(item.remaining_amount)}<small>{item.currency} remaining</small></span>
        <ChevronDown className="expand-icon" size={15} />
      </summary>
      <div className="allocations">
        <p className="eyebrow">RECEIVABLE · REVISION {item.version}</p>
        <p className="intent-note">{item.notes || "No notes recorded."}</p>
        <p className="hint">{item.due_date ? `Due ${item.due_date.slice(0, 10)}` : "No due date recorded."}</p>
        <h3>Repayment history</h3>
        {expanded ? repayments.isPending ? <div role="status" className="hint">Loading repayments…</div> : repayments.isError ? (
          <div role="alert" className="state-box error"><p>{repayments.error.message}</p><Button variant="outline" onClick={() => repayments.refetch()}>Try again</Button></div>
        ) : repayments.data?.repayments.length ? (
          <div>
            <p className="hint">Showing {repayments.data.repayments.length} of {repayments.data.total_count} · Includes voided entries</p>
            {repayments.data.repayments.map((repayment) => (
              <div className="allocation" key={repayment.id}>
                <span>{repayment.description}<small>{repayment.date.slice(0, 10)} · {accountNames.get(repayment.account_id) ?? "Account"} · {repayment.status === "void" ? "Voided" : "Posted"} · Revision {repayment.version}</small></span>
                <span>{money(repayment.amount)} {repayment.currency}</span>
              </div>
            ))}
          </div>
        ) : <p className="hint">No repayments recorded for this debt.</p> : null}
        <nav className="expense-pagination" aria-label={`Repayment pages for ${item.debtor}`}>
          <Button variant="outline" disabled={offset === 0 || repayments.isFetching} onClick={() => setOffset((value) => Math.max(0, value - 20))}>Previous</Button>
          <span>Page {Math.floor(offset / 20) + 1}{repayments.data ? ` of ${Math.max(1, Math.ceil(repayments.data.total_count / 20))}` : ""}</span>
          <Button variant="outline" disabled={!repayments.data || offset + 20 >= repayments.data.total_count || repayments.isFetching} onClick={() => setOffset((value) => value + 20)}>Next</Button>
        </nav>
      </div>
    </details>
  );
}

function currentReportingMonth() {
  const parts = new Intl.DateTimeFormat("en", { timeZone: "Europe/Bucharest", year: "numeric", month: "2-digit" }).formatToParts(new Date());
  return `${parts.find((part) => part.type === "year")?.value}-${parts.find((part) => part.type === "month")?.value}`;
}
function MonthlyReportPanel({ token, accounts }: { token: string; accounts: components["schemas"]["Account"][] }) {
  const [month, setMonth] = useState(currentReportingMonth);
  const [accountID, setAccountID] = useState("");
  const report = useQuery({
    queryKey: ["monthly-report", month, accountID],
    enabled: Boolean(month),
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/reports/monthly", {
        params: { query: { month, account_id: accountID } },
        headers: authHeaders(token), signal, cache: "no-store",
      });
      if (!response.ok || !data) throw new Error(response.status === 422 || response.status === 400 ? "Choose a valid month for this report." : "The monthly report could not be loaded. Check your connection and access, then try again.");
      return data;
    },
  });
  return (
    <section className="buying-intents" aria-labelledby="monthly-heading">
      <div className="section-heading"><h2 id="monthly-heading">Monthly report</h2><span>Calendar month · Europe/Bucharest</span></div>
      <div className="expense-filter-grid">
        <label>Reporting month<Input type="month" value={month} onChange={(event) => setMonth(event.target.value)} /></label>
        <label>Reporting account<select value={accountID} onChange={(event) => setAccountID(event.target.value)}><option value="">All accounts</option>{accounts.map((account) => <option key={account.id} value={account.id}>{account.name}{account.archived ? " (archived)" : ""}</option>)}</select></label>
      </div>
      {!month ? <p className="empty">Choose a month to view the report.</p> : report.isPending ? <div className="state-box" role="status">Loading monthly report…</div> : report.isError ? (
        <div className="state-box error" role="alert"><p>{report.error.message}</p><Button variant="outline" onClick={() => report.refetch()}>Try again</Button></div>
      ) : report.data ? (
        <>
          <p className="intent-intro">{report.data.from_date} through {report.data.to_date} · Posted entries only. Income and spending retain their original currencies.</p>
          <div className="monthly-grid">
            <div><h3>Income</h3>{report.data.income_totals?.length ? report.data.income_totals.map((total) => <p className="intent-price" key={total.currency}>{money(total.amount)} {total.currency}</p>) : <p className="hint">No income this month.</p>}</div>
            <div><h3>Spending</h3>{report.data.spending_totals?.length ? report.data.spending_totals.map((total) => <p className="intent-price" key={total.currency}>{money(total.amount)} {total.currency}</p>) : <p className="hint">No spending this month.</p>}</div>
          </div>
          <div className="monthly-grid">
            <details><summary>Spending by category</summary>{report.data.category_totals?.length ? report.data.category_totals?.map((total) => <div className="allocation" key={`${total.id}:${total.currency}`}><span>{total.name}</span><span>{money(total.amount)} {total.currency}</span></div>) : <p className="hint">No category spending.</p>}</details>
            <details><summary>Spending by project</summary>{report.data.project_totals?.length ? report.data.project_totals.map((total) => <div className="allocation" key={`${total.id}:${total.currency}`}><span>{total.name || "Unassigned"}</span><span>{money(total.amount)} {total.currency}</span></div>) : <p className="hint">No project spending.</p>}</details>
          </div>
          <h3>Account movements</h3>
          <p className="intent-intro">Each account uses its own currency. Transfers, debt repayments and balance adjustments stay separate from income and spending. Opening balances are excluded.</p>
          <div className="expense-list">
            {report.data.accounts?.length ? report.data.accounts.map((account) => (
              <details className="expense" key={account.account_id}>
                <summary>
                  <span className="expense-icon"><Wallet size={18} /></span>
                  <span className="expense-description"><strong>{account.name}</strong><span>{account.balance_quality === "incomplete" ? "Incomplete movement total" : account.balance_quality === "estimated" ? "Estimated movement total" : "Exact movement total"}</span></span>
                  <span className="expense-amount">{account.net_change.startsWith("-") ? "" : "+"}{money(account.net_change)}<small>{account.currency} {account.balance_quality === "incomplete" ? "known net change" : "net change"}</small></span>
                  <ChevronDown className="expand-icon" size={15} />
                </summary>
                <div className="allocations">
                  {account.balance_quality !== "exact" ? <p className="intent-note">{account.estimated_expense_count} estimated expense payments · {account.unresolved_expense_count} unresolved expense payments. {account.balance_quality === "incomplete" ? "Unresolved debits are excluded, so this net change is incomplete." : "Net change includes estimated debits and may change when bank charges are confirmed."}</p> : null}
                  {([
                    ["Income", account.income], ["Expense debits", account.expense_debits], ["Transfers received", account.transfer_in], ["Transfers sent", account.transfer_out], ["Debt repayments received", account.repayments], ["Balance adjustments", account.adjustments],
                  ] as const).map(([label, amount]) => <div className="allocation" key={label}><span>{label}</span><span>{money(amount)} {account.currency}</span></div>)}
                  <p className="hint">Net change adds income, transfers received, debt repayments and signed adjustments, then subtracts expense debits and transfers sent.</p>
                </div>
              </details>
            )) : <p className="empty">No accounts in this selection.</p>}
          </div>
        </>
      ) : null}
    </section>
  );
}

function HistoryFilters({ label, filters, accounts, includeCurrency = false, onChange }: { label: string; filters: typeof defaultHistoryFilters; accounts: components["schemas"]["Account"][]; includeCurrency?: boolean; onChange: (filters: typeof defaultHistoryFilters) => void }) {
  function change(key: keyof typeof defaultHistoryFilters, value: string) { onChange({ ...filters, [key]: value }); }
  return (
    <details className="expense-filters">
      <summary>Filter {label}</summary>
      <div className="expense-filter-grid">
        <label>{label === "transfers" ? "Account (sent or received)" : "Account"}<select value={filters.account_id} onChange={(event) => change("account_id", event.target.value)}><option value="">All accounts</option>{accounts.map((account) => <option key={account.id} value={account.id}>{account.name}{account.archived ? " (archived)" : ""}</option>)}</select></label>
        {includeCurrency ? <label>Currency<select value={filters.currency} onChange={(event) => change("currency", event.target.value)}><option value="">All currencies</option>{[...new Set(["RON", "EUR", "USD", ...accounts.map((account) => account.currency)])].map((currency) => <option key={currency} value={currency}>{currency}</option>)}</select></label> : null}
        <label>From date<Input type="date" value={filters.from_date} onChange={(event) => change("from_date", event.target.value)} /></label>
        <label>Through date<Input type="date" value={filters.to_date} onChange={(event) => change("to_date", event.target.value)} /></label>
        <label>Status<select value={filters.status} onChange={(event) => change("status", event.target.value)}><option value="posted">Posted</option><option value="void">Voided</option><option value="all">All</option></select></label>
      </div>
      <Button variant="ghost" onClick={() => onChange(defaultHistoryFilters)}>Reset {label} filters</Button>
    </details>
  );
}
function HistoryPages({ label, offset, total, busy, onChange }: { label: string; offset: number; total: number | undefined; busy: boolean; onChange: (offset: number) => void }) {
  return <nav className="expense-pagination" aria-label={`${label} pages`}>
    <Button variant="outline" disabled={offset === 0 || busy} onClick={() => onChange(Math.max(0, offset - 20))}>Previous</Button>
    <span>Page {Math.floor(offset / 20) + 1}{total !== undefined ? ` of ${Math.max(1, Math.ceil(total / 20))}` : ""}</span>
    <Button variant="outline" disabled={total === undefined || offset + 20 >= total || busy} onClick={() => onChange(offset + 20)}>Next</Button>
  </nav>;
}

function ProjectReportPanel({ token, projects, categories }: { token: string; projects: components["schemas"]["Named"][]; categories: components["schemas"]["Named"][] }) {
  const [projectID, setProjectID] = useState("");
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");
  const [offset, setOffset] = useState(0);
  const categoryNames = new Map(categories.map((category) => [category.id, category.name]));
  const report = useQuery({
    queryKey: ["project-report", projectID, fromDate, toDate, offset],
    enabled: Boolean(projectID),
    queryFn: async ({ signal }) => {
      const { data, response } = await api.GET("/api/projects/{id}/report", {
        params: { path: { id: projectID }, query: { from_date: fromDate, to_date: toDate, limit: 20, offset } },
        headers: authHeaders(token), signal, cache: "no-store",
      });
      if (!response.ok || !data) throw new Error(response.status === 422 || response.status === 400 ? "Check the project date range: the start must not follow the end." : "The project report could not be loaded. Try again.");
      return data;
    },
  });
  return <section className="buying-intents" aria-labelledby="project-report-heading">
    <div className="section-heading"><h2 id="project-report-heading">Project report</h2><span>Spending only</span></div>
    <div className="expense-filter-grid">
      <label>Report project<select value={projectID} onChange={(event) => { setProjectID(event.target.value); setOffset(0); }}><option value="">Choose a project</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}{project.archived ? " (archived)" : ""}</option>)}</select></label>
      <label>Project from<Input type="date" value={fromDate} onChange={(event) => { setFromDate(event.target.value); setOffset(0); }} /></label>
      <label>Project through<Input type="date" value={toDate} onChange={(event) => { setToDate(event.target.value); setOffset(0); }} /></label>
      <Button variant="outline" onClick={() => { setFromDate(""); setToDate(""); setOffset(0); }}>Reset project dates</Button>
    </div>
    {!projectID ? <p className="empty">Choose a project to inspect its spending and expense history.</p> : report.isPending ? <div className="state-box" role="status">Loading project report…</div> : report.isError ? <div className="state-box error" role="alert"><p>{report.error.message}</p><Button variant="outline" onClick={() => report.refetch()}>Try again</Button></div> : report.data ? <>
      <h3>{report.data.project.name}{report.data.project.archived ? " · Archived" : ""}</h3>
      <p className="intent-intro">Posted allocations assigned to this project, across all matching pages. Currencies are kept separate.</p>
      {report.data.spending_totals?.length ? report.data.spending_totals.map((total) => <p className="intent-price" key={total.currency}>{money(total.amount)} {total.currency}</p>) : <p className="empty">No project spending in this period.</p>}
      <details><summary>Project category breakdown</summary>{report.data.category_totals?.map((total) => <div className="allocation" key={`${total.category_id}:${total.currency}`}><span>{categoryNames.get(total.category_id) ?? "Category"}</span><span>{money(total.amount)} {total.currency}</span></div>)}</details>
      <h3>Project expense history</h3><p className="hint">Full purchase amounts are shown for context. Only the project allocations below each purchase contribute to project spending.</p>
      <div className="expense-list">{report.data.history.expenses?.map((expense) => <details className="expense" key={expense.id}>
        <summary><span className="expense-icon"><ArrowUpRight size={18} /></span><span className="expense-description"><strong>{expense.description}</strong><span>{expense.date}</span></span><span className="expense-amount">{money(expense.amount)}<small>{expense.currency} full purchase</small></span><ChevronDown className="expand-icon" size={15} /></summary>
        <div className="allocations">{expense.allocations?.filter((allocation) => allocation.project_id === projectID).map((allocation, index) => <div className="allocation" key={index}><span>{categoryNames.get(allocation.category_id) ?? "Category"} · Project allocation</span><span>{money(allocation.amount)} {expense.currency}</span></div>)}</div>
      </details>)}</div>
      {!report.data.history.expenses?.length && report.data.history.total_count > 0 ? <p className="empty">No expenses on this page. Use Previous to return to earlier records.</p> : null}
      <nav className="expense-pagination" aria-label="Project expense pages"><Button variant="outline" disabled={offset === 0 || report.isFetching} onClick={() => setOffset((value) => Math.max(0, value - 20))}>Previous</Button><span>Page {Math.floor(offset / 20) + 1}</span><Button variant="outline" disabled={offset + 20 >= report.data.history.total_count || report.isFetching} onClick={() => setOffset((value) => value + 20)}>Next</Button></nav>
    </> : null}
  </section>;
}
