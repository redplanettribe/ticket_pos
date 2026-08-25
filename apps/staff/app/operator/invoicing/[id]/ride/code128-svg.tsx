import { code128Modules } from "@/lib/code128";

// The clave de acceso as a Code 128 barcode in SVG (#456): one rect per run
// of bars, a ten-module quiet zone either side, and the value on the element
// so what the barcode encodes can be read without scanning it.

const QUIET_ZONE_MODULES = 10;

type Code128SvgProps = {
  value: string;
  /** Height in user units; the width follows the module count. */
  height?: number;
  className?: string;
};

export function Code128Svg({ value, height = 44, className }: Code128SvgProps) {
  const modules = code128Modules(value);
  const width = modules.length + QUIET_ZONE_MODULES * 2;
  const bars: { x: number; width: number }[] = [];
  let run = 0;
  for (let i = 0; i <= modules.length; i += 1) {
    if (modules[i] === "1") {
      run += 1;
      continue;
    }
    if (run > 0) {
      bars.push({ x: QUIET_ZONE_MODULES + i - run, width: run });
      run = 0;
    }
  }
  return (
    <svg
      className={className}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      role="img"
      aria-label={value}
      data-code128={value}
      shapeRendering="crispEdges"
    >
      <rect x={0} y={0} width={width} height={height} fill="#fff" />
      {bars.map((bar) => (
        <rect key={bar.x} x={bar.x} y={0} width={bar.width} height={height} fill="#000" />
      ))}
    </svg>
  );
}
