"use client"

import * as React from "react"
import * as TabsPrimitive from "@radix-ui/react-tabs"
import { cn } from "@/lib/utils"

const Tabs = TabsPrimitive.Root

const TabsList = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List> & {
    variant?: "default" | "line"
  }
>(({ className, variant = "default", ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={cn(
      variant === "default" &&
      "inline-flex h-8 items-center justify-center gap-0.5 rounded-lg border border-border bg-muted/70 p-[3px] text-muted-foreground",
      variant === "line" &&
      "inline-flex h-9 items-center justify-start border-b bg-transparent p-0 text-muted-foreground w-full gap-5",
      className
    )}
    {...props}
  />
))
TabsList.displayName = TabsPrimitive.List.displayName

const TabsTrigger = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger> & {
    variant?: "default" | "line"
  }
>(({ className, variant = "default", ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      variant === "default" &&
      "inline-flex h-full items-center justify-center gap-1.5 whitespace-nowrap rounded-md px-2.5 text-[13px] font-medium transition-[color,background-color,box-shadow] hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25 disabled:pointer-events-none disabled:opacity-50 data-[state=active]:bg-card data-[state=active]:text-foreground data-[state=active]:shadow-control data-[state=active]:ring-1 data-[state=active]:ring-border dark:data-[state=active]:bg-accent [&_svg]:size-3.5",
      variant === "line" &&
      "-mb-px inline-flex h-full items-center justify-center gap-1.5 whitespace-nowrap rounded-none border-b-2 border-transparent px-0.5 text-sm font-medium transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25 disabled:pointer-events-none disabled:opacity-50 data-[state=active]:border-foreground data-[state=active]:text-foreground data-[state=active]:shadow-none",
      className
    )}
    {...props}
  />
))
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName

const TabsContent = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content
    ref={ref}
    className={cn(
      "mt-2 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25",
      className
    )}
    {...props}
  />
))
TabsContent.displayName = TabsPrimitive.Content.displayName

export { Tabs, TabsList, TabsTrigger, TabsContent }
