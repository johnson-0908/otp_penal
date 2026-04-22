import { Laptop, Moon, Sun } from "lucide-react";
import { useTheme } from "../hooks/useTheme";
import { cn } from "../lib/utils";
import type { Theme } from "../lib/theme";

const opts: { value: Theme; icon: typeof Sun; label: string }[] = [
  { value: "light", icon: Sun, label: "浅色" },
  { value: "system", icon: Laptop, label: "跟随系统" },
  { value: "dark", icon: Moon, label: "深色" },
];

export function ThemeToggle({ className }: { className?: string }) {
  const [theme, setTheme] = useTheme();
  return (
    <div
      role="radiogroup"
      aria-label="主题"
      className={cn(
        "inline-flex rounded-md border bg-background p-0.5",
        className,
      )}
    >
      {opts.map((o) => {
        const Icon = o.icon;
        const active = theme === o.value;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={active}
            title={o.label}
            onClick={() => setTheme(o.value)}
            className={cn(
              "inline-flex h-7 w-7 items-center justify-center rounded-sm transition-colors",
              "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
              active
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-accent/50",
            )}
          >
            <Icon className="h-3.5 w-3.5" />
          </button>
        );
      })}
    </div>
  );
}
