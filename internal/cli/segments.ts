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
    description: "Create a single task. For two or more tasks, prefer seg_add_many -- one call, fewer tokens.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      title: Type.String({ description: "Task title" }),
      body: Type.Optional(Type.String({ description: "Self-contained description: what to do, file paths, constraints, expected outcome" })),
      priority: Type.Optional(Type.Number({ description: "0-3 (0=none, 1=low, 2=medium, 3=high); use numbers, not words" })),
      blocked_by: Type.Optional(Type.String({ description: "Task ID of a hard blocker (task that must land first)" })),
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
    description: "Create multiple tasks in one call. Preferred for planning -- scaffold a whole queue in one round-trip. In blocked_by, '#0'..'#N' reference earlier tasks in this batch.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      tasks: Type.Array(Type.Object({
        title: Type.String(),
        body: Type.Optional(Type.String()),
        priority: Type.Optional(Type.Number()),
        blocked_by: Type.Optional(Type.String({ description: "Task ID or '#<index>' of an earlier entry in this batch" })),
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
    description: "Update a task. Only provided fields change.",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      task_id: Type.String({ description: "Task ID" }),
      title: Type.Optional(Type.String({ description: "New title" })),
      body: Type.Optional(Type.String({ description: "New body" })),
      status: Type.Optional(Type.String({ description: "todo | in_progress | done | closed | blocker" })),
      priority: Type.Optional(Type.Number({ description: "0-3" })),
      blocked_by: Type.Optional(Type.String({ description: "Task ID of a blocker" })),
    }),
    handler: async ({ project_id, task_id, title = "", body = "", status = "", priority, blocked_by = "" }: any) => {
      const p = typeof priority === "number" ? priority : -1;
      return request(`/api/tasks/${task_id}`, "PUT", { project_id, title, body, status, priority: p, blocked_by });
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
    description: "Delete a task",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      task_id: Type.String({ description: "Task ID" }),
    }),
    handler: async ({ project_id, task_id }: { project_id: string; task_id: string }) => {
      return request(`/api/tasks/${task_id}?project_id=${project_id}`, "DELETE");
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
      `  Planning           Break a feature or refactor into steps BEFORE coding. Use seg_add_many to stub the whole queue in one call.`,
      `  Scaffolding        Stub upcoming work as todo tasks so the queue is visible.`,
      `  Starting work      seg_update status=in_progress on the task you pick up. Keep at most one in_progress at a time.`,
      `  Finishing          seg_done when the work lands.`,
      `  New scope          Capture every "we should also..." as a new todo task immediately.`,
      `  "segment it" / "sg it" / "seg it" / "segment this" / "sg this" / "seg this"`,
      `                     Capture the current topic as a task right now, no clarifying questions.`,
      ``,
      `Task body is the contract. Every body must be self-contained: what to do, relevant file paths, constraints, expected outcome. A fresh session with no history must be able to pick it up from the body alone.`,
      ``,
      `Priority (integer 0-3; use numbers, not "high"/"medium"/"low"):`,
      `  0 default/backlog  1 low  2 medium  3 high (do before other work or unblocks others)`,
      `Leave at 0 unless the user signals urgency or the task clearly gates others.`,
      ``,
      `Blockers (blocked_by): set ONLY when task A literally cannot start until task B lands. Leave empty for soft ordering -- priority and list order already handle that. Never create cycles.`,
      ``,
      `Tools:`,
      `  seg_tasks(project_id, status?)`,
      `  seg_add(project_id, title, body?, priority?, blocked_by?)`,
      `  seg_add_many(project_id, tasks: [{title, body?, priority?, blocked_by?}, ...])`,
      `                                                      Preferred for planning. Use "#0".."#N" in blocked_by to reference earlier tasks in this batch.`,
      `  seg_update(project_id, task_id, title?, body?, status?, priority?, blocked_by?)`,
      `                                                      status: todo | in_progress | done | closed | blocker`,
      `  seg_done(project_id, task_id)`,
      `  seg_rm(project_id, task_id)`,
      ``,
      `If the segments MCP server is also configured, equivalent tools are segments_list_tasks, segments_create_task, segments_create_tasks, segments_update_task, segments_get_task, segments_delete_task. The MCP tools accept an optional project_id -- if omitted they auto-resolve from CWD basename.`,
      ``,
      `IDs below are full UUIDs ready to paste into tool calls.`,
    ].join("\n");
    try {
      const projects = await request("/api/projects") as any[];
      if (!projects.length) return;
      const lines = [shortcuts, ""];
      for (const p of projects) {
        const tasks = await request(`/api/projects/${p.id}/tasks`) as any[];
        const counts = { todo: 0, in_progress: 0, done: 0, blocker: 0 };
        const open: string[] = [];
        for (const t of tasks) {
          if (t.status in counts) (counts as any)[t.status]++;
          if (t.status === "todo" || t.status === "in_progress" || t.status === "blocker") {
            let entry = `  [${t.status}] ${t.title}  task_id=${t.id}`;
            if (t.priority > 0) entry += ` P${t.priority}`;
            if (t.blocked_by) entry += ` blocked_by=${t.blocked_by}`;
            open.push(entry);
          }
        }
        lines.push(`Project: ${p.name}  project_id=${p.id}  (${tasks.length} tasks: ${counts.todo} todo, ${counts.in_progress} in progress, ${counts.done} done, ${counts.blocker} blockers)`);
        lines.push(...open);
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