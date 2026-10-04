// Ported from istok-cli-astro-docs/src/components/Logo.tsx to keep the brand mark identical.

const ratios = [1, 0.8, 0.6, 0.4, 0.2]
const opacities = [0.1, 0.3, 0.5, 0.8, 1]

export function Logo({ size = 28 }: { size?: number }) {
  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      {ratios.map((ratio, index) => (
        <div
          key={ratio}
          className="absolute top-1/2 left-1/2 rounded-full bg-emerald-600 dark:bg-emerald-500"
          style={{
            width: size * ratio,
            height: size * ratio,
            opacity: opacities[index],
            transform: "translate(-50%, -50%)",
          }}
        />
      ))}
    </div>
  )
}
