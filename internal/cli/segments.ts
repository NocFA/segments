import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { Type } from "@sinclair/typebox";

const BASE_URL = process.env.SEGMENTS_URL || "http://localhost:8765";

export default function (pi: ExtensionAPI) {
  async function request(path: string, method = "GET", body?: object): Promise<any> {
    const args = ["-s", "-f", "-X", method];
    if (body) {
      args.push("-H", "Content-Type: application/json", "-d", JSON.stringify(body));
    }
    args.push(BASE_URL + path);
    const result = await pi.exec("curl", args);
    if (result.code !== 0) {
      throw new Error("Server not running at " + BASE_URL);
    }
    return JSON.parse(result.stdout);
  }
  pi.registerTool({
    name: "seg_tasks",
    description: "List tasks for a project",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      status: Type.Optional(Type.String({ description: "Filter by status" })),
    }),
    handler: async ({ project_id, status }: { project_id: string; status?: string }) => {
      const tasks = await request(`/api/projects/${project_id}/tasks`) as any[];
      if (status) return tasks.filter(t => t.status === status);
      return tasks;
    },
  });

  pi.registerTool({
    name: "seg_add",
    description: "Create a single task. For two or more tasks, prefer seg_add_many -- one call, fewer tokens. Always pass priority (1/2/3) and set blocked_by when a hard dependency exists.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      title: Type.String({ description: "Task title" }),
      body: Type.Optional(Type.String({ description: "Self-contained description: what to do, file paths, constraints, expected outcome" })),
      priority: Type.Optional(Type.Number({ description: "Integer 1, 2, or 3 -- pick one every time you create. 1=URGENT (\"drop everything\", broken build, blocking other work). 2=NORMAL (regular session work; default when unsure). 3=BACKLOG (someday/idea/future). 0 is legacy-unset -- do NOT pick 0 when creating." })),
      blocked_by: Type.Optional(Type.String({ description: "Task ID of a hard blocker. REQUIRED whenever this task literally cannot start until the blocker lands (bootstrap -> downstream, Install X -> Use X, schema -> feature, discovered-from parent -> child). Leave empty only for genuinely independent tasks." })),
    }),
    handler: async ({ project_id, title, body = "", priority = 0, blocked_by }: { project_id: string; title: string; body?: string; priority?: number; blocked_by?: string }) => {
      const t = await request(`/api/projects/${project_id}/tasks`, "POST", { title, body, priority });
      if (blocked_by) {
        return request(`/api/tasks/${t.id}`, "PUT", { project_id, title: "", body: "", status: "", priority: -1, blocked_by });
      }
      return t;
    },
  });

  pi.registerTool({
    name: "seg_add_many",
    description: "Create multiple tasks in one call. PREFERRED for planning/scaffolding -- scaffold a whole queue in one round-trip. Set priority (1/2/3) on every entry. In blocked_by, '#0'..'#N' references earlier entries in this batch. Link obvious dependency chains: for a greenfield scaffold, put the bootstrap/init task at #0 and every downstream task gets blocked_by=\"#0\". Creating a scaffold batch without linking obvious dependencies is a correctness mistake, not a style choice.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      tasks: Type.Array(Type.Object({
        title: Type.String({ description: "Task title" }),
        body: Type.Optional(Type.String({ description: "Self-contained description: what to do, file paths, constraints, expected outcome" })),
        priority: Type.Optional(Type.Number({ description: "Integer 1, 2, or 3 -- pick one per task. 1=URGENT, 2=NORMAL (default), 3=BACKLOG. Do NOT pick 0 when creating." })),
        blocked_by: Type.Optional(Type.String({ description: "Task ID or '#<index>' of an earlier entry in this batch. Use '#0' when everything depends on a bootstrap task. REQUIRED whenever this task cannot start until the blocker lands." })),
      })),
    }),
    handler: async ({ project_id, tasks }: { project_id: string; tasks: Array<{ title: string; body?: string; priority?: number; blocked_by?: string }> }) => {
      const created: any[] = [];
      for (let i = 0; i < tasks.length; i++) {
        const spec = tasks[i];
        const t = await request(`/api/projects/${project_id}/tasks`, "POST", {
          title: spec.title,
          body: spec.body ?? "",
          priority: spec.priority ?? 0,
        });
        let blockedBy = spec.blocked_by || "";
        if (blockedBy.startsWith("#")) {
          const idx = parseInt(blockedBy.slice(1), 10);
          if (!isNaN(idx) && idx >= 0 && idx < created.length) {
            blockedBy = created[idx].id;
          } else {
            throw new Error(`tasks[${i}].blocked_by=${spec.blocked_by}: no earlier batch entry at that index`);
          }
        }
        if (blockedBy) {
          const updated = await request(`/api/tasks/${t.id}`, "PUT", {
            project_id, title: "", body: "", status: "", priority: -1, blocked_by: blockedBy,
          });
          created.push(updated);
        } else {
          created.push(t);
        }
      }
      return created;
    },
  });

  pi.registerTool({
    name: "seg_update",
    description: "Update a single task. For two or more updates, prefer seg_update_many -- one call, fewer tokens, atomic claim semantics. Only provided fields change. Use status=in_progress to claim a task when you start work and status=done when it lands.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      task_id: Type.String({ description: "Task ID" }),
      title: Type.Optional(Type.String({ description: "New title" })),
      body: Type.Optional(Type.String({ description: "New body" })),
      status: Type.Optional(Type.String({ description: "todo | in_progress | done | closed | blocker. Set in_progress when you claim/pick a task up; done when the work lands." })),
      priority: Type.Optional(Type.Number({ description: "Integer. 1=URGENT (drop everything / blocking work). 2=NORMAL (regular session work). 3=BACKLOG (someday/idea/future). 0=unset is legacy-only." })),
      blocked_by: Type.Optional(Type.String({ description: "Task ID of a hard blocker (empty to clear). Set whenever this task literally cannot start until the blocker lands." })),
    }),
    handler: async ({ project_id, task_id, title = "", body = "", status = "", priority, blocked_by = "" }: any) => {
      const p = typeof priority === "number" ? priority : -1;
      return request(`/api/tasks/${task_id}`, "PUT", { project_id, title, body, status, priority: p, blocked_by });
    },
  });

  pi.registerTool({
    name: "seg_update_many",
    description: "Update multiple tasks in one call. PREFERRED whenever you are changing two or more tasks. Use this to CLAIM a sequence of tasks (status=in_progress on each) up front when the user hands you multiple task IDs to work through -- all downstream agents see the claim atomically instead of racing. Also use it to mark several tasks done at session end. Per-entry fields follow seg_update semantics.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      updates: Type.Array(Type.Object({
        task_id: Type.String({ description: "Task ID to update" }),
        title: Type.Optional(Type.String({ description: "New title" })),
        body: Type.Optional(Type.String({ description: "New body" })),
        status: Type.Optional(Type.String({ description: "todo | in_progress | done | closed | blocker. Set in_progress to claim; done when work lands." })),
        priority: Type.Optional(Type.Number({ description: "Integer 1/2/3. 1=URGENT, 2=NORMAL, 3=BACKLOG. 0=unset is legacy-only." })),
        blocked_by: Type.Optional(Type.String({ description: "Task ID of a hard blocker (empty to clear)." })),
      })),
    }),
    handler: async ({ project_id, updates }: { project_id: string; updates: Array<{ task_id: string; title?: string; body?: string; status?: string; priority?: number; blocked_by?: string }> }) => {
      const results: any[] = [];
      for (let i = 0; i < updates.length; i++) {
        const u = updates[i];
        if (!u.task_id) throw new Error(`updates[${i}].task_id is required`);
        const p = typeof u.priority === "number" ? u.priority : -1;
        const t = await request(`/api/tasks/${u.task_id}`, "PUT", {
          project_id,
          title: u.title ?? "",
          body: u.body ?? "",
          status: u.status ?? "",
          priority: p,
          blocked_by: u.blocked_by ?? "",
        });
        results.push(t);
      }
      return results;
    },
  });

  pi.registerTool({
    name: "seg_done",
    description: "Mark task as done",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      task_id: Type.String({ description: "Task ID" }),
    }),
    handler: async ({ project_id, task_id }: { project_id: string; task_id: string }) => {
      return request(`/api/tasks/${task_id}`, "PUT", { project_id, title: "", body: "", status: "done", priority: 0 });
    },
  });

  pi.registerTool({
    name: "seg_rm",
    description: "Delete a single task. For two or more deletes, prefer seg_rm_many.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      task_id: Type.String({ description: "Task ID" }),
    }),
    handler: async ({ project_id, task_id }: { project_id: string; task_id: string }) => {
      return request(`/api/tasks/${task_id}?project_id=${project_id}`, "DELETE");
    },
  });

  pi.registerTool({
    name: "seg_rm_many",
    description: "Delete multiple tasks in one call. PREFERRED whenever removing two or more tasks.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      task_ids: Type.Array(Type.String({ description: "Task ID" })),
    }),
    handler: async ({ project_id, task_ids }: { project_id: string; task_ids: string[] }) => {
      const deleted: string[] = [];
      for (let i = 0; i < task_ids.length; i++) {
        const id = task_ids[i];
        if (!id) throw new Error(`task_ids[${i}] must be a non-empty string`);
        await request(`/api/tasks/${id}?project_id=${project_id}`, "DELETE");
        deleted.push(id);
      }
      return { deleted };
    },
  });

  pi.registerCommand("seg", {
    description: "Show Segments status",
    handler: async (args: string, ctx: any) => {
      try {
        const projects = await request("/api/projects") as any[];
        if (!projects.length) {
          ctx.ui.notify("No projects. Use sg add -p <id> <title> to create tasks.", "info");
          return;
        }
        let out = `Projects: ${projects.length}\n`;
        for (const p of projects) {
          const tasks = await request(`/api/projects/${p.id}/tasks`) as any[];
          const done = tasks.filter(t => t.status === "done").length;
          out += `  ${p.name}: ${done}/${tasks.length} done\n`;
        }
        ctx.ui.notify(out, "info");
      } catch (e) {
        // Server not running — fall back to CLI
        try {
          const out = await pi.exec("sg", ["list"]);
          ctx.ui.notify(String(out).trim() || "No projects yet.", "info");
        } catch {
          ctx.ui.notify("Segments not available. Run: sg start", "error");
        }
      }
    },
  });

  pi.on("before_agent_start", async (event, ctx) => {
    const shortcuts = [
      `Segments is the persistent task tracker for this project. Tasks survive context wipes and outlive sessions. Use Segments to plan multi-step work, scaffold upcoming tasks, track what is in progress, and capture follow-ups so they are not lost.`,
      ``,
      `When to use it (proactively, without being asked):`,
      `  Planning           Break a feature or refactor into steps BEFORE coding. Use seg_add_many to stub the whole queue in ONE call with priority + blocked_by on every entry.`,
      `  Scaffolding        Stub upcoming work as todo tasks so the queue is visible.`,
      `  Starting / claiming work`,
      `                     Pick from the Ready queue (unblocked todos, listed below). IMMEDIATELY set status=in_progress to "claim" the task so other agents/sessions do not pick up the same work. If the user hands you multiple task IDs to work in sequence, claim ALL of them up front via seg_update_many (bulk) so every one is marked in_progress before you start task one, then process them one at a time. Claim only what you will actually work this session; revert unwanted claims back to todo so others can pick them up.`,
      `  Finishing          seg_done when the work lands (or seg_update_many to mark several done at once).`,
      `  New scope          Capture every "we should also..." as a new todo immediately. If the follow-up was discovered while working on task X and cannot start until X lands, set blocked_by=<X's id> (the "discovered-from" pattern).`,
      `  "segment it" / "sg it" / "seg it" / "segment this" / "sg this" / "seg this"`,
      `                     Capture the current topic as a task right now, no clarifying questions.`,
      ``,
      `Task body is the contract. Every body must be self-contained: what to do, relevant file paths, constraints, expected outcome. A fresh session with no history must be able to pick it up from the body alone.`,
      ``,
      `Priority is an integer 1, 2, or 3 -- pick one every time you CREATE a task. Use numbers, NOT the words "high"/"medium"/"low". Match the user's signal:`,
      `  1  URGENT. "drop everything and fix X", "this is blocking prod", "broken build", "critical bug". Also: any task actively blocking other ready work.`,
      `  2  NORMAL. "let's do X", "add Y", "refactor Z" -- regular session work. Default to 2 when the intent is clearly "do this now or next" but not urgent.`,
      `  3  BACKLOG. "sometime we should", "maybe later", "one idea is", "let's discuss". Not this session.`,
      `  0 is unset (legacy only). Never pick 0 when creating. If unsure, default to 2.`,
      ``,
      `blocked_by is a correctness signal, not a hint. Set blocked_by=<task_id> whenever task A literally cannot start until task B lands. Omitting it when there is a real hard dependency misleads the next agent about which task is actionable in the Ready queue.`,
      `  MUST be set in these cases:`,
      `    - Greenfield scaffold: the bootstrap/init task blocks every downstream task. In a seg_add_many batch, put init at #0 and give every other task blocked_by="#0".`,
      `    - Infra before feature: "Install Stripe SDK" blocks "Build Merch page".`,
      `    - Schema before use: "Add DB migration" blocks "Wire up form submission".`,
      `    - Discovered-from: follow-up task discovered while working on X and cannot start until X is done -> blocked_by=<X's id>.`,
      `  Leave blocked_by empty only for genuinely independent tasks. Soft "do this after that" ordering is handled by priority and list order, not blocked_by. Never create cycles. Creating a scaffold batch without linking the obvious dependency chain is a correctness mistake, not a style choice.`,
      ``,
      `Tools: prefer the _many bulk variants whenever you touch two or more tasks -- one call, fewer tokens, atomic claim semantics.`,
      `  seg_tasks(project_id, status?)`,
      `  seg_add(project_id, title, body?, priority=1|2|3, blocked_by?)`,
      `  seg_add_many(project_id, tasks: [{title, body?, priority=1|2|3, blocked_by?}, ...])`,
      `                                                      Preferred for planning. Use "#0".."#N" in blocked_by to reference earlier tasks in this batch.`,
      `  seg_update(project_id, task_id, title?, body?, status?, priority?, blocked_by?)`,
      `                                                      status: todo | in_progress | done | closed | blocker`,
      `  seg_update_many(project_id, updates: [{task_id, title?, body?, status?, priority?, blocked_by?}, ...])`,
      `                                                      Preferred for claiming a run of tasks or marking several done at once.`,
      `  seg_done(project_id, task_id)`,
      `  seg_rm(project_id, task_id)`,
      `  seg_rm_many(project_id, task_ids: [id, id, ...])`,
      ``,
      `If the segments MCP server is also configured, equivalent tools are segments_list_tasks, segments_create_task, segments_create_tasks, segments_update_task, segments_update_tasks, segments_get_task, segments_delete_task, segments_delete_tasks. The MCP tools accept an optional project_id -- if omitted they auto-resolve from CWD basename.`,
      ``,
      `Ready queue = todos whose blocker is empty or done. Pick from there first. IDs below are full UUIDs ready to paste into tool calls.`,
    ].join("\n");
    try {
      const projects = await request("/api/projects") as any[];
      if (!projects.length) return;
      const lines = [shortcuts, ""];
      for (const p of projects) {
        const tasks = await request(`/api/projects/${p.id}/tasks`) as any[];
        const byId: Record<string, any> = Object.fromEntries(tasks.map(t => [t.id, t]));
        const counts = { todo: 0, in_progress: 0, done: 0, blocker: 0 };
        const inProgress: string[] = [];
        const ready: string[] = [];
        const blocked: string[] = [];
        const blockers: string[] = [];
        const format = (t: any) => {
          let entry = `    [${t.status}] ${t.title}  task_id=${t.id}`;
          if (t.priority > 0) entry += ` P${t.priority}`;
          if (t.blocked_by) entry += ` blocked_by=${t.blocked_by}`;
          return entry;
        };
        for (const t of tasks) {
          if (t.status in counts) (counts as any)[t.status]++;
          if (t.status === "in_progress") {
            inProgress.push(format(t));
          } else if (t.status === "todo") {
            const blocker = t.blocked_by ? byId[t.blocked_by] : null;
            if (!t.blocked_by || (blocker && blocker.status === "done")) {
              ready.push(format(t));
            } else {
              blocked.push(format(t));
            }
          } else if (t.status === "blocker") {
            blockers.push(format(t));
          }
        }
        lines.push(`Project: ${p.name}  project_id=${p.id}  (${tasks.length} tasks: ${counts.todo} todo [${ready.length} ready], ${counts.in_progress} in progress, ${counts.done} done, ${counts.blocker} blockers)`);
        if (inProgress.length) { lines.push("  In progress:"); lines.push(...inProgress); }
        if (ready.length)      { lines.push("  Ready queue (unblocked -- pick from here):"); lines.push(...ready); }
        if (blocked.length)    { lines.push("  Blocked (waiting on a dependency):"); lines.push(...blocked); }
        if (blockers.length)   { lines.push("  Blockers (status=blocker, investigate):"); lines.push(...blockers); }
      }
      return {
        message: {
          customType: "segments-context",
          content: lines.join("\n"),
          display: false,
        },
      };
    } catch {}
  });
}