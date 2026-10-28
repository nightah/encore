import type { ComponentProps, ReactNode } from "react";

import { Button as BaseButton } from "@base-ui/react/button";
import { type LinkComponent, createLink } from "@tanstack/react-router";
import { LoaderCircle } from "lucide-react";

import { cn, focusRing } from "@components/ui/cn";

export type ButtonVariant = "default" | "primary" | "danger" | "ghost";

const base =
  "inline-flex min-h-11 select-none items-center justify-center gap-2 whitespace-nowrap rounded-lg border px-4 font-medium text-sm no-underline transition-colors disabled:cursor-not-allowed disabled:opacity-60 data-disabled:cursor-not-allowed data-disabled:opacity-60";

const variants: Record<ButtonVariant, string> = {
  danger: "border-bad-bg bg-bad-bg text-bad hover:border-bad",
  default: "border-btn-border bg-surface text-ink hover:bg-btn-hover hover:text-ink",
  ghost: "border-transparent bg-transparent text-ink hover:bg-btn-hover",
  primary: "border-primary bg-primary text-primary-ink hover:border-primary-hover hover:bg-primary-hover",
};

export function buttonClass(variant: ButtonVariant = "default", className?: string) {
  return cn(base, variants[variant], focusRing, className);
}

interface ButtonProps extends ComponentProps<typeof BaseButton> {
  variant?: ButtonVariant;
  /** Shows a spinner and disables the button while an async action runs. */
  pending?: boolean;
  icon?: ReactNode;
}

export function Button({
  variant = "default",
  pending = false,
  icon,
  className,
  children,
  disabled,
  type = "button",
  ...props
}: ButtonProps) {
  return (
    <BaseButton
      type={type}
      disabled={disabled || pending}
      aria-busy={pending || undefined}
      className={buttonClass(variant, typeof className === "string" ? className : undefined)}
      {...props}
    >
      {pending ? <LoaderCircle aria-hidden="true" className="size-4 animate-spin" /> : icon}
      {children}
    </BaseButton>
  );
}

interface IconButtonProps extends Omit<ButtonProps, "children" | "icon" | "className"> {
  label: string;
  className?: string;
  children: ReactNode;
}

/** Square 44px button with an accessible name; the visible content is just an icon. */
export function IconButton({ label, className, children, variant = "default", ...props }: IconButtonProps) {
  return (
    <Button aria-label={label} title={label} variant={variant} className={cn("size-11 px-0", className)} {...props}>
      {children}
    </Button>
  );
}

interface AnchorButtonProps extends ComponentProps<"a"> {
  variant?: ButtonVariant;
}

// ref arrives as an ordinary prop (React 19), which createLink relies on.
function StyledAnchor({ variant = "default", className, ...props }: AnchorButtonProps) {
  return <a className={buttonClass(variant, className)} {...props} />;
}

/** A router link that looks like a button. */
export const ButtonLink: LinkComponent<typeof StyledAnchor> = createLink(StyledAnchor);

/** A plain anchor (downloads, external pages) that looks like a button. */
export const AnchorButton = StyledAnchor;
