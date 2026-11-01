import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Table, Td, Th } from "@components/ui/Table";

describe("Table", () => {
  it("gives each labelled cell its column's name for the phone layout, and no label to the rest", () => {
    render(
      <Table aria-label="Echos">
        <thead>
          <tr>
            <Th>Name</Th>
            <Th>Room</Th>
            <Th>Actions</Th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <Td lead>Bedroom Echo</Td>
            <Td label="Room">Bedroom</Td>
            <Td action>Edit</Td>
          </tr>
        </tbody>
      </Table>,
    );

    expect(screen.getByRole("cell", { name: "Bedroom" })).toHaveAttribute("data-label", "Room");
    expect(screen.getByRole("cell", { name: "Bedroom Echo" })).not.toHaveAttribute("data-label");
    expect(screen.getByRole("cell", { name: "Edit" })).not.toHaveAttribute("data-label");
  });
});
