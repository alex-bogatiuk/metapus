import * as React from "react"
import { Slot } from "@radix-ui/react-slot"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md border border-transparent text-sm font-medium transition-[color,background-color,border-color,box-shadow] duration-150 focus-visible:outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/25 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground shadow-sm hover:bg-primary/90",
        destructive:
          "border-destructive/25 bg-destructive/10 text-destructive hover:border-transparent hover:bg-destructive hover:text-destructive-foreground",
        outline:
          "border-input bg-card text-foreground shadow-control hover:bg-muted",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-accent",
        success:
          "bg-success text-success-foreground shadow-sm hover:bg-success/90",
        ghost: "text-foreground/85 hover:bg-accent hover:text-accent-foreground",
        link: "text-info underline-offset-4 hover:underline",
      },
      size: {
        default: "h-8 px-3",
        sm: "h-7 px-2.5 text-[13px] [&_svg]:size-3.5",
        lg: "h-9 px-4",
        icon: "size-8",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
  VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button"
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    )
  }
)
Button.displayName = "Button"

export { Button, buttonVariants }
