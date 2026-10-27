export function FormError({ message }: { message: string | undefined }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
      {message}
    </p>
  );
}
