"use client";

import { usePathname } from "next/navigation";
import { Component, type ReactNode } from "react";

import { isNavigationSignal, type CaughtError } from "@/lib/error-screen";

import { RouteErrorScreen } from "./error-screen";

type BoundaryProps = { pathname: string; children: ReactNode };
type BoundaryState = { error: CaughtError | null; pathname: string };

/**
 * The boundary a Next `error.tsx` would be, placed where Next cannot put one: in
 * the content area of StaffPageShell, which each page draws for itself. A crash
 * in a page's content renders the route-level screen inside the panels instead
 * of taking them down with it (#772).
 *
 * Like Next's own boundary it lets `notFound()` and `redirect()` through to
 * Next, and forgets the error when the person navigates elsewhere.
 */
class Boundary extends Component<BoundaryProps, BoundaryState> {
  state: BoundaryState = { error: null, pathname: this.props.pathname };

  static getDerivedStateFromError(error: unknown): Partial<BoundaryState> {
    if (isNavigationSignal(error)) {
      throw error;
    }
    return { error: error as CaughtError };
  }

  static getDerivedStateFromProps(props: BoundaryProps, state: BoundaryState): Partial<BoundaryState> | null {
    if (props.pathname !== state.pathname) {
      return { error: null, pathname: props.pathname };
    }
    return null;
  }

  reset = () => this.setState({ error: null });

  render() {
    if (this.state.error) {
      return <RouteErrorScreen error={this.state.error} reset={this.reset} layout="inline" />;
    }
    return this.props.children;
  }
}

export function ContentErrorBoundary({ children }: { children: ReactNode }) {
  return <Boundary pathname={usePathname()}>{children}</Boundary>;
}
