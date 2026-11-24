export const ForgeHooks = async ({ $, directory }) => {
  return {
    "tool.execute.before": async (input, output) => {
      if (input.tool === "bash" && output.args.command) {
        const guardPath = directory + "/.claude/hooks/guard";
        const { exitCode: exists } = await $`test -f ${guardPath}`.nothrow();
        if (exists === 0) {
          const { exitCode } = await $`bash ${guardPath} --command ${output.args.command}`.nothrow();
          if (exitCode !== 0) {
            throw new Error("Command blocked by forge guard hook");
          }
        }
      }
    },
    "session.created": async () => {
      await $`bd prime || true`.nothrow();
      await $`instill sync || true`.nothrow();
      await $`command -v forge >/dev/null 2>&1 && forge sync-allowlist --check || true`.nothrow();
      await $`command -v forge >/dev/null 2>&1 && forge upgrade --check || true`.nothrow();
    },
  };
};