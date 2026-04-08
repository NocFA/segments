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
    description: "Create a new task",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      title: Type.String({ description: "Task title" }),
      body: Type.Optional(Type.String({ description: "Task body" })),
      priority: Type.Optional(Type.Number({ description: "Priority (0-3)" })),
    }),
    handler: async ({ project_id, title, body = "", priority = 0 }: { project_id: string; title: string; body?: string; priority?: number }) => {
      return request(`/api/projects/${project_id}/tasks`, "POST", { title, body, priority });
    },
  });

  pi.registerTool({
    name: "seg_update",
    description: "Update a task",
    parameters: Type.Object({
      project_id: Type.String({ description: "Project ID" }),
      task_id: Type.String({ description: "Task ID" }),
      title: Type.Optional(Type.String({ description: "New title" })),
      body: Type.Optional(Type.String({ description: "New body" })),
      status: Type.Optional(Type.String({ description: "New status" })),
      priority: Type.Optional(Type.Number({ description: "New priority (0-3)" })),
    }),
    handler: async ({ project_id, task_id, title = "", body = "", status, priority = 0 }: any) => {
      return request(`/api/tasks/${task_id}`, "PUT", { project_id, title, body, status, priority });
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
    try {
      const projects = await request("/api/projects") as any[];
      if (projects.length) {
        const project = projects[0];
        const tasks = await request(`/api/projects/${project.id}/tasks`) as any[];
        const todo = tasks.filter(t => t.status === "todo");
        if (todo.length) {
          return {
            message: {
              customType: "segments-context",
              content: `[Current project: ${project.name}, ${todo.length} pending tasks]`,
              display: false,
            },
          };
        }
      }
    } catch {}
  });
}