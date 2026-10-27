// BASE_URL is "./", so this resolves against the <base href> the Go server injects, deep links included.
const markUrl = `${import.meta.env.BASE_URL}brand/mark.svg`;

export function LogoMark({ className, size = 32 }: { className?: string; size?: number }) {
  return <img src={markUrl} alt="" width={size} height={size} className={className} />;
}
